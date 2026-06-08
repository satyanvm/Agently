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
	"os"
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
	if to := os.Getenv("NOTIFICATION_EMAIL_TO"); to != "" {
		chans = append(chans, &email{to: to, log: log})
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

// email is structured + logged for now. The trigger and payload are correct; a
// real SMTP / provider (SES, Resend, Postmark) send drops in behind Send().
type email struct {
	to  string
	log Logger
}

func (m *email) Name() string { return "email" }

func (m *email) Send(ctx context.Context, e Event) error {
	subject := fmt.Sprintf("Agently: %s #%d %s", e.WorkflowName, e.Number, e.Status)
	m.log.Info("email notification (logged; SMTP send is a follow-up)",
		"to", m.to, "subject", subject, "url", e.URL)
	return nil
}
