package services

import (
	"testing"
	"time"
)

func TestScheduleFrom(t *testing.T) {
	cases := []struct {
		prompt string
		want   string // "" means nil
	}{
		{"every day at 9 am i want startup news emailed to satyanvm7@gmail.com", "daily 09:00"},
		{"each morning send me a digest", "daily 09:00"},      // morning, no time → 09:00
		{"daily at 9:30pm email me", "daily 21:30"},           // meridiem + minutes
		{"every day at 17:00 send the report", "daily 17:00"}, // 24h
		{"email me at 8 every morning", "daily 08:00"},        // "at 8"
		{"send updates hourly", "hourly"},
		{"every hour give me news", "hourly"},
		{"just email me the news", ""}, // no cadence → no schedule
		{"daily at 12 am", "daily 00:00"},
		{"daily at 12 pm", "daily 12:00"},
	}
	for _, c := range cases {
		got := scheduleFrom(c.prompt)
		gotStr := ""
		if got != nil {
			gotStr = *got
		}
		if gotStr != c.want {
			t.Errorf("scheduleFrom(%q) = %q, want %q", c.prompt, gotStr, c.want)
		}
	}
}

func TestDueAt(t *testing.T) {
	loc := time.UTC
	// "now" = 2026-06-20 10:15 UTC
	now := time.Date(2026, 6, 20, 10, 15, 0, 0, loc)

	// daily 09:00 has passed today → due, fire at today 09:00.
	fire, ok := dueAt("daily 09:00", now, loc)
	if !ok || !fire.Equal(time.Date(2026, 6, 20, 9, 0, 0, 0, loc)) {
		t.Errorf("daily 09:00 @10:15: ok=%v fire=%v, want due @09:00", ok, fire)
	}

	// daily 11:00 is still in the future today → not due yet.
	if _, ok := dueAt("daily 11:00", now, loc); ok {
		t.Errorf("daily 11:00 @10:15 should NOT be due yet")
	}

	// hourly → always due, fire at top of current hour (10:00).
	fire, ok = dueAt("hourly", now, loc)
	if !ok || !fire.Equal(time.Date(2026, 6, 20, 10, 0, 0, 0, loc)) {
		t.Errorf("hourly @10:15: ok=%v fire=%v, want @10:00", ok, fire)
	}

	// garbage schedule → not parseable.
	if _, ok := dueAt("whenever", now, loc); ok {
		t.Errorf("unparseable schedule should not be due")
	}
}
