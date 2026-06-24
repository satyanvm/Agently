// Package notifier delivers run-completion notifications to the OUTSIDE world —
// the "close your laptop, get pinged when it's done" promise. It's separate from
// the in-app notification row (that's written directly to the DB): this is the
// push to an external channel.
//
// Channels are selected by env. Webhooks fan out (all fire). EMAIL is different:
// several email backends may be configured, but a recipient should get exactly
// ONE email, so the emailers are tried in precedence order and the first one that
// applies to the event wins:
//
//	1. gmail-oauth — send AS the user's connected Gmail (option 2). Applies only
//	   when the run's workspace has connected Google (the runner threads the
//	   refresh token onto the Event). Needs GOOGLE_CLIENT_ID/SECRET.
//	2. resend     — send FROM the app's own verified domain (option 3). Always
//	   applies when RESEND_API_KEY + EMAIL_FROM are set. The standard SaaS path.
//	3. smtp       — raw SMTP (e.g. Gmail App Password). The original fallback.
//
// No channel configured ⇒ a no-op notifier (the in-app DB row still happens).
package notifier

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/smtp"
	"net/url"
	"os"
	"strings"
	"time"
)

// Event is the payload delivered when a run finishes.
type Event struct {
	RunID        string `json:"runId"`
	WorkflowName string `json:"workflowName"`
	WorkflowSlug string `json:"workflowSlug"`
	Number       int    `json:"number"`
	Status       string `json:"status"` // succeeded | failed | canceled
	URL          string `json:"url"`    // deep link to the run in the UI
	Digest       string `json:"digest"` // the run's result content (e.g. the AI digest)
	To           string `json:"-"`      // per-run recipient override (run.input.email)

	// GmailRefreshToken/GmailSender are set by the runner when the run's workspace
	// has connected a Google account (option 2). When present, the gmail-oauth
	// channel sends the email AS that account. Empty ⇒ that channel doesn't apply.
	GmailRefreshToken string `json:"-"`
	GmailSender       string `json:"-"`
}

// Logger is the small logging surface notifiers use.
type Logger interface {
	Info(msg string, args ...any)
	Error(msg string, args ...any)
}

// Channel is one delivery mechanism.
type Channel interface {
	Name() string
	Send(ctx context.Context, e Event) error
}

// EmailChannel is a Channel that can say whether it applies to a given event, so
// the notifier can pick exactly one email backend per event (precedence order).
type EmailChannel interface {
	Channel
	Applicable(e Event) bool
}

// Notifier fans webhooks out and sends a single email via the best backend.
type Notifier struct {
	webhooks []Channel
	emailers []EmailChannel // precedence order: gmail-oauth, resend, smtp
	log      Logger
}

// New builds a Notifier from the environment.
func New(log Logger) *Notifier {
	n := &Notifier{log: log}

	if u := os.Getenv("NOTIFICATION_WEBHOOK_URL"); u != "" {
		n.webhooks = append(n.webhooks, &webhook{url: u, http: &http.Client{Timeout: 10 * time.Second}})
	}

	httpc := &http.Client{Timeout: 20 * time.Second}

	// 1) Gmail OAuth2 (option 2). The channel exists whenever the OAuth app creds
	// are present; per-event applicability depends on the workspace having connected
	// Google (Event.GmailRefreshToken). The recipient comes from the run input.
	if id, secret := os.Getenv("GOOGLE_CLIENT_ID"), os.Getenv("GOOGLE_CLIENT_SECRET"); id != "" && secret != "" {
		n.emailers = append(n.emailers, &gmailOAuth{clientID: id, clientSecret: secret, http: httpc})
	}
	// 2) Resend (option 3): send from the app's own domain. EMAIL_FROM must be on a
	// domain verified in Resend (e.g. "Agently <digests@yourdomain.com>").
	if key := os.Getenv("RESEND_API_KEY"); key != "" {
		n.emailers = append(n.emailers, &resend{
			apiKey: key, from: envOr("EMAIL_FROM", os.Getenv("SMTP_FROM")),
			defaultTo: os.Getenv("NOTIFICATION_EMAIL_TO"), http: httpc,
		})
	}
	// 3) SMTP fallback (Gmail App Password etc.).
	if host := os.Getenv("SMTP_HOST"); host != "" {
		n.emailers = append(n.emailers, &email{
			host: host, port: envOr("SMTP_PORT", "587"),
			user: os.Getenv("SMTP_USER"), pass: os.Getenv("SMTP_PASS"),
			from:      envOr("SMTP_FROM", os.Getenv("SMTP_USER")),
			defaultTo: os.Getenv("NOTIFICATION_EMAIL_TO"),
		})
	}
	return n
}

// Notify delivers the event: every webhook fires (best-effort), and the first
// applicable email backend sends one email. Failures are logged, never fatal —
// a missed ping must not fail a completed run.
func (n *Notifier) Notify(ctx context.Context, e Event) {
	if len(n.webhooks) == 0 && len(n.emailers) == 0 {
		n.log.Info("no notification channel configured; in-app only", "runId", e.RunID)
		return
	}
	for _, w := range n.webhooks {
		if err := w.Send(ctx, e); err != nil {
			n.log.Error("notification delivery failed", "channel", w.Name(), "runId", e.RunID, "error", err.Error())
			continue
		}
		n.log.Info("notification delivered", "channel", w.Name(), "runId", e.RunID, "status", e.Status)
	}
	for _, c := range n.emailers {
		if !c.Applicable(e) {
			continue
		}
		if err := c.Send(ctx, e); err != nil {
			n.log.Error("notification delivery failed", "channel", c.Name(), "runId", e.RunID, "error", err.Error())
		} else {
			n.log.Info("notification delivered", "channel", c.Name(), "runId", e.RunID, "status", e.Status)
		}
		return // exactly one email backend per event
	}
}

/* ------------------------------- webhook --------------------------------- */

type webhook struct {
	url  string
	http *http.Client
}

func (w *webhook) Name() string { return "webhook" }

func (w *webhook) Send(ctx context.Context, e Event) error {
	body, _ := json.Marshal(e)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("content-type", "application/json")
	resp, err := w.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("webhook status %d", resp.StatusCode)
	}
	return nil
}

/* --------------------------- gmail oauth2 (opt 2) ------------------------- */

// gmailOAuth sends mail AS a connected Google account using a stored refresh
// token. It mints a short-lived access token from the refresh token, then calls
// the Gmail API users.messages.send. No password ever touches the app.
type gmailOAuth struct {
	clientID, clientSecret string
	http                   *http.Client
}

func (g *gmailOAuth) Name() string { return "gmail-oauth" }

// Applicable only when this run's workspace has connected Gmail.
func (g *gmailOAuth) Applicable(e Event) bool { return strings.TrimSpace(e.GmailRefreshToken) != "" }

func (g *gmailOAuth) Send(ctx context.Context, e Event) error {
	if strings.TrimSpace(e.To) == "" {
		return fmt.Errorf("gmail-oauth: no recipient (run input.email)")
	}
	token, err := g.accessToken(ctx, e.GmailRefreshToken)
	if err != nil {
		return err
	}
	from := firstNonEmpty(e.GmailSender, "me")
	subject, body := renderEmail(e)
	raw := base64.URLEncoding.EncodeToString(buildMessage(from, e.To, subject, body))

	payload, _ := json.Marshal(map[string]string{"raw": raw})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://gmail.googleapis.com/gmail/v1/users/me/messages/send", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := g.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("gmail send: status %d: %s", resp.StatusCode, readErr(resp))
	}
	return nil
}

// accessToken exchanges a refresh token for a short-lived access token.
func (g *gmailOAuth) accessToken(ctx context.Context, refreshToken string) (string, error) {
	form := url.Values{
		"client_id":     {g.clientID},
		"client_secret": {g.clientSecret},
		"refresh_token": {refreshToken},
		"grant_type":    {"refresh_token"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://oauth2.googleapis.com/token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := g.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("token refresh: status %d: %s", resp.StatusCode, readErr(resp))
	}
	var out struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if out.AccessToken == "" {
		return "", fmt.Errorf("token refresh: empty access_token")
	}
	return out.AccessToken, nil
}

/* ------------------------------ resend (opt 3) --------------------------- */

// resend sends via the Resend transactional API (https://resend.com). from must
// be a sender on a domain verified in your Resend account.
type resend struct {
	apiKey, from, defaultTo string
	http                    *http.Client
}

func (r *resend) Name() string { return "resend" }

func (r *resend) Applicable(e Event) bool { return r.from != "" }

func (r *resend) Send(ctx context.Context, e Event) error {
	to := firstNonEmpty(e.To, r.defaultTo)
	if to == "" {
		return fmt.Errorf("resend: no recipient (set NOTIFICATION_EMAIL_TO or run input.email)")
	}
	subject, body := renderEmail(e)
	payload, _ := json.Marshal(map[string]any{
		"from": r.from, "to": []string{to}, "subject": subject, "text": body,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.resend.com/emails", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+r.apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("resend send: status %d: %s", resp.StatusCode, readErr(resp))
	}
	return nil
}

/* -------------------------------- smtp ----------------------------------- */

// email sends a real message over SMTP (STARTTLS via net/smtp.SendMail).
type email struct {
	host, port, user, pass, from, defaultTo string
}

func (m *email) Name() string { return "smtp" }

func (m *email) Applicable(e Event) bool { return m.host != "" }

func (m *email) Send(ctx context.Context, e Event) error {
	to := firstNonEmpty(e.To, m.defaultTo)
	if to == "" {
		return fmt.Errorf("no recipient (set NOTIFICATION_EMAIL_TO or run input.email)")
	}
	subject, body := renderEmail(e)
	msg := buildMessage(m.from, to, subject, body)
	addr := m.host + ":" + m.port
	auth := smtp.PlainAuth("", m.user, m.pass, m.host)
	if err := smtp.SendMail(addr, auth, m.from, []string{to}, msg); err != nil {
		return fmt.Errorf("smtp send: %w", err)
	}
	return nil
}

/* ------------------------------- helpers --------------------------------- */

// renderEmail builds the subject + plain-text body shared by every email backend.
func renderEmail(e Event) (subject, body string) {
	subject = fmt.Sprintf("Agently: %s #%d %s", e.WorkflowName, e.Number, e.Status)
	body = e.Digest
	if body == "" {
		body = fmt.Sprintf("Your run %s finished with status: %s.", e.WorkflowName, e.Status)
	}
	body += "\n\n—\nView the full run: " + e.URL
	return subject, body
}

func buildMessage(from, to, subject, body string) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", from)
	fmt.Fprintf(&b, "To: %s\r\n", to)
	fmt.Fprintf(&b, "Subject: %s\r\n", subject)
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	b.WriteString("\r\n")
	b.WriteString(body)
	return []byte(b.String())
}

// readErr returns a short snippet of an error response body for logging.
func readErr(resp *http.Response) string {
	buf := make([]byte, 512)
	n, _ := resp.Body.Read(buf)
	return strings.TrimSpace(string(buf[:n]))
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}
