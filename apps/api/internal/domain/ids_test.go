package domain

import (
	"regexp"
	"testing"
)

func TestNewIDFormat(t *testing.T) {
	id := NewID(KindRun)
	re := regexp.MustCompile(`^run_[0-9a-z]{20}$`)
	if !re.MatchString(id) {
		t.Fatalf("NewID(run) = %q, does not match prefix+base36 pattern", id)
	}
}

func TestNewIDUnique(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 1000; i++ {
		id := NewID(KindWorkflow)
		if seen[id] {
			t.Fatalf("collision on %q", id)
		}
		seen[id] = true
	}
}
