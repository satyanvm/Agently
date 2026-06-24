package notifier

import (
	"context"
	"testing"
)

type nopLog struct{}

func (nopLog) Info(string, ...any)  {}
func (nopLog) Error(string, ...any) {}

// fakeEmail records whether it was asked to send, and can declare applicability.
type fakeEmail struct {
	name       string
	applicable bool
	sent       bool
}

func (f *fakeEmail) Name() string            { return f.name }
func (f *fakeEmail) Applicable(e Event) bool { return f.applicable }
func (f *fakeEmail) Send(context.Context, Event) error {
	f.sent = true
	return nil
}

// TestNotify_PicksOneEmailByPrecedence proves exactly one email backend sends,
// and that the first APPLICABLE one in precedence order wins (gmail > resend > smtp).
func TestNotify_PicksOneEmailByPrecedence(t *testing.T) {
	gmail := &fakeEmail{name: "gmail-oauth"}
	resend := &fakeEmail{name: "resend", applicable: true}
	smtp := &fakeEmail{name: "smtp", applicable: true}

	n := &Notifier{log: nopLog{}, emailers: []EmailChannel{gmail, resend, smtp}}

	// Gmail not applicable (no connected account) → resend wins, smtp skipped.
	n.Notify(context.Background(), Event{To: "x@y.com"})
	if gmail.sent || !resend.sent || smtp.sent {
		t.Fatalf("expected only resend to send; gmail=%v resend=%v smtp=%v", gmail.sent, resend.sent, smtp.sent)
	}

	// Now the workspace has connected Gmail → gmail wins over everything.
	gmail.applicable, resend.sent = true, false
	n2 := &Notifier{log: nopLog{}, emailers: []EmailChannel{gmail, resend, smtp}}
	n2.Notify(context.Background(), Event{To: "x@y.com", GmailRefreshToken: "rt"})
	if !gmail.sent || resend.sent {
		t.Fatalf("expected gmail to win when connected; gmail=%v resend=%v", gmail.sent, resend.sent)
	}
}

// TestChannelApplicability checks each real channel's Applicable predicate.
func TestChannelApplicability(t *testing.T) {
	g := &gmailOAuth{}
	if g.Applicable(Event{}) {
		t.Error("gmail should not apply without a refresh token")
	}
	if !g.Applicable(Event{GmailRefreshToken: "rt"}) {
		t.Error("gmail should apply with a refresh token")
	}
	if (&resend{from: ""}).Applicable(Event{}) {
		t.Error("resend should not apply without EMAIL_FROM")
	}
	if !(&resend{from: "a@b.com"}).Applicable(Event{}) {
		t.Error("resend should apply with EMAIL_FROM")
	}
}
