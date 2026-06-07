package platform

import "time"

// Time is a dependency, not a global. Passing a Clock lets the seed be
// deterministic and keeps time.Now() out of business logic. Mirrors
// packages/core/src/platform/clock.ts.
type Clock interface {
	Now() int64  // Unix milliseconds
	ISO() string // ISO-8601 UTC string
}

type systemClock struct{}

func (systemClock) Now() int64  { return time.Now().UnixMilli() }
func (systemClock) ISO() string { return time.Now().UTC().Format(time.RFC3339) }

// SystemClock is the live clock used for new writes.
var SystemClock Clock = systemClock{}

type fixedClock struct{ ms int64 }

func (c fixedClock) Now() int64  { return c.ms }
func (c fixedClock) ISO() string { return time.UnixMilli(c.ms).UTC().Format(time.RFC3339) }

// FixedClock returns a clock frozen at the given ISO instant — used to seed
// deterministic data.
func FixedClock(at string) Clock {
	t, err := time.Parse(time.RFC3339, at)
	if err != nil {
		panic("FixedClock: invalid time " + at + ": " + err.Error())
	}
	return fixedClock{ms: t.UnixMilli()}
}
