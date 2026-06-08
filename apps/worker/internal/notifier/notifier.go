// Package notifier delivers run-completion notifications to the OUTSIDE world —
// the "close your laptop, get pinged when it's done" promise. It's separate from
// the in-app notification row (that's written directly to the DB): this is the
// push to an external channel.
//
// Channels are selected by env, and a Notifier can hold several at once:
//   - webhook: POSTs a JSON payload to NOTIFICATION_WEBHOOK_URL. Fully real and
//     locally testable (point it at any HTTP listener).
//   - email:   structured + logged; a real SMTP/provider send is a small follow-up
//     (the payload and trigger are already correct).
//
// No channel configured ⇒ a no-op notifier (the in-app DB row still happens).
package notifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/smtp"
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

// Notifier fans an event out to all configured channels.
type Notifier struct {
	channels []Channel
	log      Logger
}

// New builds a Notifier from the environment.
func New(log Logger) *Notifier {
	var chans []Channel
	if url := os.Getenv("NOTIFICATION_WEBHOOK_URL"); url != "" {
		chans = append(chans, &webhook{url: url, http: &http.Client{Timeout: 10 * time.Second}})
	}
	// Email via SMTP. Configured with SMTP_HOST/PORT/USER/PASS/FROM (Gmail: host
	// smtp.gmail.com:587, user = your address, pass = an app password). A default
	// recipient comes from NOTIFICATION_EMAIL_TO; a run can override via input.email.
	if host := os.Getenv("SMTP_HOST"); host != "" {
		chans = append(chans, &email{
			host:      host,
			port:      envOr("SMTP_PORT", "587"),
			user:      os.Getenv("SMTP_USER"),
			pass:      os.Getenv("SMTP_PASS"),
			from:      envOr("SMTP_FROM", os.Getenv("SMTP_USER")),
			defaultTo: os.Getenv("NOTIFICATION_EMAIL_TO"),
			log:       log,
		})
	}
	return &Notifier{channels: chans, log: log}
}

// Notify delivers the event on every channel (best-effort; failures are logged,
// never fatal — a missed ping must not fail a completed run).
func (n *Notifier) Notify(ctx context.Context, e Event) {
	if len(n.channels) == 0 {
		n.log.Info("no notification channel configured; in-app only", "runId", e.RunID)
		return
	}
	for _, c := range n.channels {
		if err := c.Send(ctx, e); err != nil {
			n.log.Error("notification delivery failed", "channel", c.Name(), "runId", e.RunID, "error", err.Error())
			continue
		}
		n.log.Info("notification delivered", "channel", c.Name(), "runId", e.RunID, "status", e.Status)
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

/* -------------------------------- email ---------------------------------- */

// email sends a real message over SMTP (STARTTLS via net/smtp.SendMail). The body
// carries the run's digest, so "email me the AI digest" actually delivers content.
type email struct {
	host, port, user, pass, from, defaultTo string
	log                                     Logger
}

func (m *email) Name() string { return "email" }

func (m *email) Send(ctx context.Context, e Event) error {
	to := firstNonEmpty(e.To, m.defaultTo)
	if to == "" {
		return fmt.Errorf("no recipient (set NOTIFICATION_EMAIL_TO or run input.email)")
	}
	subject := fmt.Sprintf("Agently: %s #%d %s", e.WorkflowName, e.Number, e.Status)
	body := e.Digest
	if body == "" {
		body = fmt.Sprintf("Your run %s finished with status: %s.", e.WorkflowName, e.Status)
	}
	body += "\n\n—\nView the full run: " + e.URL

	msg := buildMessage(m.from, to, subject, body)
	addr := m.host + ":" + m.port
	auth := smtp.PlainAuth("", m.user, m.pass, m.host)
	// net/smtp.SendMail does EHLO + STARTTLS + AUTH + send (works with Gmail :587).
	if err := smtp.SendMail(addr, auth, m.from, []string{to}, msg); err != nil {
		return fmt.Errorf("smtp send: %w", err)
	}
	return nil
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
