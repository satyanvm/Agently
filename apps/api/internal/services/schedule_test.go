package services

import (
	"strings"
	"testing"
	"time"

	"github.com/agently/api/internal/domain"
	"github.com/agently/api/internal/platform"
)

// 2026-10-19 is a Monday.
func at(day, hh, mm int) time.Time { return time.Date(2026, 10, day, hh, mm, 0, 0, time.UTC) }

func TestParseScheduleForms(t *testing.T) {
	valid := map[string][]time.Time{ // schedule → times that must fire
		"hourly":              {at(19, 10, 0), at(20, 0, 0)},
		"daily 09:00":         {at(19, 9, 0), at(25, 9, 0)},
		"Daily  09:00":        {at(19, 9, 0)},
		"weekdays 08:30":      {at(19, 8, 30), at(23, 8, 30)},
		"weekly mon 09:00":    {at(19, 9, 0), at(26, 9, 0)},
		"weekly Monday 09:00": {at(19, 9, 0)},
		"weekly fri 17:45":    {at(23, 17, 45)},
		"0 9 * * 1-5":         {at(19, 9, 0), at(23, 9, 0)},
		"cron 0 9 * * MON":    {at(19, 9, 0)},
		"*/15 * * * *":        {at(19, 9, 0), at(19, 9, 45)},
		"0 9 1 * *":           {time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)},
		"30 6 * jan-mar sun":  {time.Date(2027, 1, 3, 6, 30, 0, 0, time.UTC)},
		"0 12 * * 7":          {at(25, 12, 0)},                                               // 7 = Sunday
		"0 0 13 * fri":        {at(23, 0, 0), time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC)}, // either day rule
		"5,35 9-10/1 * * *":   {at(19, 9, 5), at(19, 10, 35)},
		"0 9/4 * * *":         {at(19, 9, 0), at(19, 13, 0), at(19, 21, 0)},
	}
	for s, fires := range valid {
		spec, err := parseSchedule(s)
		if err != nil {
			t.Errorf("%q: unexpected error %v", s, err)
			continue
		}
		for _, f := range fires {
			if !spec.matches(f) {
				t.Errorf("%q should fire at %s (%s)", s, f.Format(time.RFC3339), f.Weekday())
			}
		}
	}
	not := map[string][]time.Time{
		"weekdays 08:30":   {at(24, 8, 30), at(25, 8, 30)}, // Sat, Sun
		"weekly mon 09:00": {at(20, 9, 0), at(19, 9, 1)},
		"0 9 * * 1-5":      {at(24, 9, 0)},
		"0 9 1 * *":        {at(19, 9, 0)},
		"0 9/4 * * *":      {at(19, 10, 0)},
	}
	for s, times := range not {
		spec, _ := parseSchedule(s)
		for _, f := range times {
			if spec.matches(f) {
				t.Errorf("%q should NOT fire at %s", s, f.Format(time.RFC3339))
			}
		}
	}
	for _, bad := range []string{
		"", "whenever", "daily", "daily 25:00", "daily 9am", "weekly 09:00", "weekly funday 09:00",
		"0 9 * *", "60 * * * *", "* 24 * * *", "* * 0 * *", "* * * 13 *", "* * * * 8",
		"*/0 * * * *", "5-1 * * * *", "0 9 * * foo",
	} {
		if err := ValidateSchedule(bad); err == nil {
			t.Errorf("%q should be rejected", bad)
		}
	}
	if err := ValidateSchedule("0 9 30 feb *"); err == nil || !strings.Contains(err.Error(), "never fires") {
		t.Errorf("30 February should never fire, got %v", err)
	}
}

func TestDueAtWindowAndNext(t *testing.T) {
	loc := time.UTC
	// Monday 09:20: the 09:00 weekly fire is due (once — alreadyFired dedupes).
	if f, ok := dueAt("weekly mon 09:00", at(19, 9, 20), loc); !ok || !f.Equal(at(19, 9, 0)) {
		t.Errorf("weekly mon 09:00 @Mon 09:20 = %v %v", f, ok)
	}
	// Missed by the API being down: still runs within 12 hours…
	if f, ok := dueAt("weekly mon 09:00", at(19, 20, 59), loc); !ok || !f.Equal(at(19, 9, 0)) {
		t.Errorf("12h catch-up: %v %v", f, ok)
	}
	// …but not the next day.
	if _, ok := dueAt("weekly mon 09:00", at(20, 9, 20), loc); ok {
		t.Error("a weekly fire should not run a day late")
	}
	// Cron: */15 at 10:07 → 10:00.
	if f, ok := dueAt("*/15 * * * *", at(19, 10, 7), loc); !ok || !f.Equal(at(19, 10, 0)) {
		t.Errorf("*/15 @10:07 = %v %v", f, ok)
	}
	// Timezone: daily 09:00 in New York is 13:00 UTC in October.
	ny, _ := time.LoadLocation("America/New_York")
	if f, ok := dueAt("daily 09:00", at(19, 13, 30), ny); !ok || !f.Equal(at(19, 13, 0)) {
		t.Errorf("daily 09:00 America/New_York @13:30Z = %v %v", f, ok)
	}
	if n, ok := NextFire("weekly mon 09:00", at(19, 9, 0)); !ok || !n.Equal(at(26, 9, 0)) {
		t.Errorf("next after a fire = %v %v", n, ok)
	}
	if n, ok := NextFire("weekdays 09:00", at(23, 10, 0)); !ok || !n.Equal(at(26, 9, 0)) {
		t.Errorf("weekdays after Friday = %v", n)
	}
}

func TestScheduleFromWeekly(t *testing.T) {
	for prompt, want := range map[string]string{
		"every monday morning send me a competitor digest":     "weekly mon 09:00",
		"every Friday at 5pm draft next week's content plan":   "weekly fri 17:00",
		"weekly, compile the top 5 opportunities":              "weekly mon 09:00",
		"every weekday at 8:30 check competitor pricing pages": "weekdays 08:30",
		"on business days at 7am":                              "weekdays 07:00",
		"today, summarise the news":                            "",
	} {
		got := ""
		if s := scheduleFrom(strings.ToLower(prompt)); s != nil {
			got = *s
		}
		if got != want {
			t.Errorf("scheduleFrom(%q) = %q, want %q", prompt, got, want)
		}
		if got != "" {
			if err := ValidateSchedule(got); err != nil {
				t.Errorf("scheduleFrom produced an invalid schedule %q: %v", got, err)
			}
		}
	}
}

// The schedule node's cron field is what the scheduler runs once the graph is
// saved; switching the trigger to manual stops it.
func TestSaveGraphDrivesTheScheduler(t *testing.T) {
	clock := platform.FixedClock("2026-10-19T09:05:00Z")
	plat := NewPlatform(Options{Clock: clock})
	seed, ok := plat.Repos.Workflows.GetBySlug("ai-digest")
	if !ok {
		t.Fatal("seed workflow missing")
	}
	wf := seed
	wf.ID, wf.Slug, wf.Trigger, wf.Schedule = domain.NewWorkflowId(), "weekly-plan", domain.TriggerManual, nil
	plat.Repos.Workflows.Insert(wf)

	graph := func(trigger domain.GraphNode) []domain.GraphNode {
		return []domain.GraphNode{trigger,
			{Key: "write", Type: "agent.llm", DependsOn: []string{"start"}, Config: map[string]any{"prompt": "x"}}}
	}
	scheduled := domain.GraphNode{Key: "start", Type: "trigger.schedule", Config: map[string]any{"cron": "weekly mon 09:00"}}

	if _, err := plat.Workflows.SaveGraph("weekly-plan", graph(domain.GraphNode{Key: "start", Type: "trigger.schedule",
		Config: map[string]any{"cron": "every so often"}})); err == nil || !strings.Contains(err.Error(), "not a schedule") {
		t.Fatalf("invalid cron should be rejected, got %v", err)
	}
	if _, err := plat.Workflows.SaveGraph("weekly-plan", graph(domain.GraphNode{Key: "start", Type: "trigger.schedule"})); err == nil ||
		!strings.Contains(err.Error(), "no schedule") {
		t.Fatalf("a schedule trigger with no schedule should be rejected, got %v", err)
	}
	if _, err := plat.Workflows.SaveGraph("weekly-plan", graph(scheduled)); err != nil {
		t.Fatal(err)
	}
	got, _ := plat.Repos.Workflows.GetBySlug("weekly-plan")
	if got.Trigger != domain.TriggerSchedule || got.Schedule == nil || *got.Schedule != "weekly mon 09:00" {
		t.Fatalf("after save: trigger=%s schedule=%v", got.Trigger, got.Schedule)
	}

	sched := plat.NewScheduler("UTC")
	runs := func() int { return len(plat.Repos.Runs.ListByWorkflow(wf.ID)) }
	sched.Tick(at(20, 9, 5)) // Tuesday: nothing
	if runs() != 0 {
		t.Fatalf("fired on a Tuesday")
	}
	sched.Tick(at(19, 9, 5)) // Monday 09:05
	sched.Tick(at(19, 9, 6)) // and again: no double fire
	if runs() != 1 {
		t.Fatalf("Monday: %d runs, want 1", runs())
	}

	// Re-saving with the cron cleared keeps the stored schedule.
	if _, err := plat.Workflows.SaveGraph("weekly-plan", graph(domain.GraphNode{Key: "start", Type: "trigger.schedule"})); err != nil {
		t.Fatalf("re-save without cron: %v", err)
	}
	// Switching to a manual trigger turns the schedule off (the string is kept).
	if _, err := plat.Workflows.SaveGraph("weekly-plan", graph(domain.GraphNode{Key: "start", Type: "trigger.manual"})); err != nil {
		t.Fatal(err)
	}
	got, _ = plat.Repos.Workflows.GetBySlug("weekly-plan")
	if got.Trigger != domain.TriggerManual || got.Schedule == nil {
		t.Fatalf("after switching to manual: trigger=%s schedule=%v", got.Trigger, got.Schedule)
	}
	sched.Tick(time.Date(2026, 10, 26, 9, 5, 0, 0, time.UTC)) // next Monday
	if runs() != 1 {
		t.Fatalf("a manual workflow must not be scheduled: %d runs", runs())
	}
}

func TestWithScheduleOnTrigger(t *testing.T) {
	s := "daily 09:00"
	nodes := withScheduleOnTrigger([]domain.GraphNode{{Key: "t", Type: "trigger.schedule"}, {Key: "a", Type: "agent.llm"}}, &s)
	if nodes[0].Config["cron"] != "daily 09:00" || nodes[1].Config != nil {
		t.Fatalf("got %#v", nodes)
	}
	keep := withScheduleOnTrigger([]domain.GraphNode{{Key: "t", Type: "trigger.schedule", Config: map[string]any{"cron": "hourly"}}}, &s)
	if keep[0].Config["cron"] != "hourly" {
		t.Fatalf("an explicit cron must win, got %#v", keep[0].Config)
	}
}
