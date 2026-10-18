package services

// schedule.go parses the schedule strings a workflow can carry and answers
// "when did this last fire?" for the scheduler (scheduler.go).
//
// Accepted forms (case-insensitive), all compiled to one 5-field cron spec:
//
//	hourly                      → 0 * * * *
//	daily HH:MM                 → MM HH * * *
//	weekdays HH:MM              → MM HH * * 1-5
//	weekly <day> HH:MM          → MM HH * * <day>      (mon, monday, tue, …)
//	<5-field cron>              → minute hour day-of-month month day-of-week
//	cron <5-field cron>         (same, explicit)
//
// Cron fields take *, n, a-b, lists (a,b), steps (*/n, a-b/n, a/n) and
// month / weekday names (jan…dec, sun…sat; 0 and 7 are both Sunday). As in
// standard cron, when both day-of-month and day-of-week are restricted a day
// matching EITHER fires.
//
// Times are wall-clock in the scheduler's zone (SCHEDULER_TZ).

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/agently/api/internal/domain"
)

// catchUpWindow bounds how late a missed fire (the API was down at the time)
// still runs: within 12 hours it runs once on the next tick; older than that
// it is skipped rather than delivered at a surprising time.
const catchUpWindow = 12 * time.Hour

type cronField struct {
	bits uint64 // bit i set → value i allowed
	star bool   // written as "*" (matters for the day-of-month / day-of-week rule)
}

func (f cronField) has(v int) bool { return f.bits&(1<<uint(v)) != 0 }

type cronSpec struct {
	minute, hour, dom, month, dow cronField
}

var monthNames = map[string]int{"jan": 1, "feb": 2, "mar": 3, "apr": 4, "may": 5, "jun": 6,
	"jul": 7, "aug": 8, "sep": 9, "oct": 10, "nov": 11, "dec": 12}

var dayNames = map[string]int{"sun": 0, "mon": 1, "tue": 2, "wed": 3, "thu": 4, "fri": 5, "sat": 6}

// dayFromWord accepts "mon", "monday", "Mon".
func dayFromWord(w string) (int, bool) {
	w = strings.ToLower(w)
	if len(w) < 3 {
		return 0, false
	}
	d, ok := dayNames[w[:3]]
	if !ok {
		return 0, false
	}
	full := map[int]string{0: "sunday", 1: "monday", 2: "tuesday", 3: "wednesday", 4: "thursday", 5: "friday", 6: "saturday"}[d]
	if len(w) > 3 && !strings.HasPrefix(full, w) {
		return 0, false
	}
	return d, true
}

// parseSchedule compiles any accepted schedule string to a cron spec.
func parseSchedule(raw string) (cronSpec, error) {
	s := strings.Join(strings.Fields(strings.ToLower(raw)), " ")
	parts := strings.Fields(s)
	if len(parts) == 0 {
		return cronSpec{}, fmt.Errorf("empty schedule")
	}
	at := func(hhmm string) (int, int, error) {
		hh, mm, ok := parseHHMM(hhmm)
		if !ok {
			return 0, 0, fmt.Errorf("%q is not a 24-hour time like 09:00", hhmm)
		}
		return hh, mm, nil
	}
	switch {
	case s == "hourly":
		return parseCron("0 * * * *")
	case parts[0] == "daily" && len(parts) == 2:
		hh, mm, err := at(parts[1])
		if err != nil {
			return cronSpec{}, err
		}
		return parseCron(fmt.Sprintf("%d %d * * *", mm, hh))
	case parts[0] == "weekdays" && len(parts) == 2:
		hh, mm, err := at(parts[1])
		if err != nil {
			return cronSpec{}, err
		}
		return parseCron(fmt.Sprintf("%d %d * * 1-5", mm, hh))
	case parts[0] == "weekly" && len(parts) == 3:
		d, ok := dayFromWord(parts[1])
		if !ok {
			return cronSpec{}, fmt.Errorf("%q is not a day of the week", parts[1])
		}
		hh, mm, err := at(parts[2])
		if err != nil {
			return cronSpec{}, err
		}
		return parseCron(fmt.Sprintf("%d %d * * %d", mm, hh, d))
	case parts[0] == "cron":
		return parseCron(strings.Join(parts[1:], " "))
	case len(parts) == 5:
		return parseCron(s)
	}
	return cronSpec{}, fmt.Errorf("%q is not a schedule: use hourly, daily HH:MM, weekdays HH:MM, weekly <day> HH:MM or a 5-field cron like 0 9 * * 1", raw)
}

func parseCron(expr string) (cronSpec, error) {
	f := strings.Fields(expr)
	if len(f) != 5 {
		return cronSpec{}, fmt.Errorf("cron needs 5 fields (minute hour day-of-month month day-of-week), got %d", len(f))
	}
	var c cronSpec
	var err error
	if c.minute, err = parseCronField(f[0], 0, 59, nil, "minute"); err != nil {
		return cronSpec{}, err
	}
	if c.hour, err = parseCronField(f[1], 0, 23, nil, "hour"); err != nil {
		return cronSpec{}, err
	}
	if c.dom, err = parseCronField(f[2], 1, 31, nil, "day-of-month"); err != nil {
		return cronSpec{}, err
	}
	if c.month, err = parseCronField(f[3], 1, 12, monthNames, "month"); err != nil {
		return cronSpec{}, err
	}
	if c.dow, err = parseCronField(f[4], 0, 7, dayNames, "day-of-week"); err != nil {
		return cronSpec{}, err
	}
	if c.dow.has(7) { // 7 is Sunday too
		c.dow.bits |= 1
		c.dow.bits &^= 1 << 7
	}
	return c, nil
}

func parseCronField(s string, lo, hi int, names map[string]int, what string) (cronField, error) {
	if s == "*" {
		var f cronField
		for v := lo; v <= hi; v++ {
			f.bits |= 1 << uint(v)
		}
		f.star = true
		return f, nil
	}
	value := func(tok string) (int, error) {
		if n, ok := names[tok]; ok {
			return n, nil
		}
		n, err := strconv.Atoi(tok)
		if err != nil || n < lo || n > hi {
			return 0, fmt.Errorf("%s %q must be %d-%d", what, tok, lo, hi)
		}
		return n, nil
	}
	var f cronField
	for _, part := range strings.Split(s, ",") {
		rng, step := part, 1
		if i := strings.Index(part, "/"); i >= 0 {
			n, err := strconv.Atoi(part[i+1:])
			if err != nil || n <= 0 {
				return cronField{}, fmt.Errorf("%s step %q must be a positive number", what, part[i+1:])
			}
			rng, step = part[:i], n
		}
		start, end := lo, hi
		switch {
		case rng == "*":
		case strings.Contains(rng, "-"):
			ab := strings.SplitN(rng, "-", 2)
			a, err := value(ab[0])
			if err != nil {
				return cronField{}, err
			}
			b, err := value(ab[1])
			if err != nil {
				return cronField{}, err
			}
			if a > b {
				return cronField{}, fmt.Errorf("%s range %q runs backwards", what, rng)
			}
			start, end = a, b
		default:
			a, err := value(rng)
			if err != nil {
				return cronField{}, err
			}
			start = a
			if step == 1 {
				end = a // a single value; "a/n" means a, a+n, … up to the max
			}
		}
		for v := start; v <= end; v += step {
			f.bits |= 1 << uint(v)
		}
	}
	return f, nil
}

// matches reports whether t (already in the schedule's zone) is a fire minute.
func (c cronSpec) matches(t time.Time) bool {
	if !c.minute.has(t.Minute()) || !c.hour.has(t.Hour()) || !c.month.has(int(t.Month())) {
		return false
	}
	domOK, dowOK := c.dom.has(t.Day()), c.dow.has(int(t.Weekday()))
	if !c.dom.star && !c.dow.star {
		return domOK || dowOK
	}
	return domOK && dowOK
}

// prev is the most recent fire minute at or before now, no older than window.
func (c cronSpec) prev(now time.Time, window time.Duration) (time.Time, bool) {
	t := time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), now.Minute(), 0, 0, now.Location())
	oldest := now.Add(-window)
	for !t.Before(oldest) {
		if c.matches(t) {
			return t, true
		}
		t = t.Add(-time.Minute)
	}
	return time.Time{}, false
}

// next is the first fire minute strictly after from (searched up to a year
// ahead); ok=false for a spec that never fires, like 30 February.
func (c cronSpec) next(from time.Time) (time.Time, bool) {
	t := time.Date(from.Year(), from.Month(), from.Day(), from.Hour(), from.Minute(), 0, 0, from.Location()).Add(time.Minute)
	for limit := from.AddDate(1, 0, 1); t.Before(limit); {
		if !c.month.has(int(t.Month())) || !c.dayMatches(t) {
			t = time.Date(t.Year(), t.Month(), t.Day()+1, 0, 0, 0, 0, t.Location())
			continue
		}
		if c.matches(t) {
			return t, true
		}
		t = t.Add(time.Minute)
	}
	return time.Time{}, false
}

func (c cronSpec) dayMatches(t time.Time) bool {
	domOK, dowOK := c.dom.has(t.Day()), c.dow.has(int(t.Weekday()))
	if !c.dom.star && !c.dow.star {
		return domOK || dowOK
	}
	return domOK && dowOK
}

// ValidateSchedule returns a user-facing error for a schedule the scheduler
// can't run, including one that can never fire.
func ValidateSchedule(raw string) error {
	c, err := parseSchedule(raw)
	if err != nil {
		return err
	}
	if _, ok := c.next(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)); !ok {
		return fmt.Errorf("%q never fires", raw)
	}
	return nil
}

// NextFire is the next time a schedule fires after from (in from's zone).
func NextFire(raw string, from time.Time) (time.Time, bool) {
	c, err := parseSchedule(raw)
	if err != nil {
		return time.Time{}, false
	}
	return c.next(from)
}

// graphSchedule reconciles a saved graph's trigger node with the workflow
// row the scheduler reads (workflows.trigger / workflows.schedule):
//
//   - a trigger.schedule node with config.cron: that string becomes the
//     workflow's schedule (validated) and the trigger becomes "schedule";
//   - a trigger.schedule node without one keeps the workflow's existing
//     schedule; with neither, the save is rejected — a schedule trigger with no
//     schedule would silently never run;
//   - a manual / webhook trigger node on a workflow that WAS scheduled turns
//     the schedule off (the string is kept, so switching back restores it).
//
// It returns the patch fields to apply (nil = leave as is).
func graphSchedule(nodes []domain.GraphNode, wf domain.Workflow) (*domain.TriggerType, **string, error) {
	for _, n := range nodes {
		switch n.Type {
		case "trigger.schedule":
			cron := strings.TrimSpace(fmt.Sprint(n.Config["cron"]))
			if n.Config["cron"] == nil {
				cron = ""
			}
			sched := domain.TriggerSchedule
			if cron == "" {
				if wf.Schedule == nil || strings.TrimSpace(*wf.Schedule) == "" {
					return nil, nil, domain.BadRequest("the schedule trigger has no schedule: set it to hourly, daily 09:00, weekly mon 09:00 or a cron like 0 9 * * 1")
				}
				return &sched, nil, nil
			}
			if err := ValidateSchedule(cron); err != nil {
				return nil, nil, domain.BadRequest("schedule: " + err.Error())
			}
			ptr := &cron
			return &sched, &ptr, nil
		case "trigger.manual", "trigger.webhook":
			if wf.Trigger != domain.TriggerSchedule {
				return nil, nil, nil
			}
			t := domain.TriggerManual
			if n.Type == "trigger.webhook" {
				t = domain.TriggerWebhook
			}
			return &t, nil, nil
		}
	}
	return nil, nil, nil
}

// withScheduleOnTrigger writes the workflow's schedule into its
// trigger.schedule node's cron field when the node has none, so the builder
// shows the schedule that will actually run.
func withScheduleOnTrigger(nodes []domain.GraphNode, schedule *string) []domain.GraphNode {
	if schedule == nil {
		return nodes
	}
	for i := range nodes {
		if nodes[i].Type != "trigger.schedule" {
			continue
		}
		if c, _ := nodes[i].Config["cron"].(string); strings.TrimSpace(c) != "" {
			continue
		}
		cfg := map[string]any{}
		for k, v := range nodes[i].Config {
			cfg[k] = v
		}
		cfg["cron"] = *schedule
		nodes[i].Config = cfg
	}
	return nodes
}
