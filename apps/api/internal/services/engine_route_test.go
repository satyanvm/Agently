package services

import (
	"testing"

	"github.com/agently/api/internal/domain"
)

func TestEngineForNodes(t *testing.T) {
	typed := []domain.GraphNode{{Key: "t", Type: "trigger.manual"}, {Key: "a", Type: "agent.llm"}}
	if got := engineForNodes(typed); got != "temporal" {
		t.Errorf("typed graph → %q, want temporal", got)
	}
	legacy := []domain.GraphNode{{Key: "arxiv"}, {Key: "editor"}}
	if got := engineForNodes(legacy); got != "native" {
		t.Errorf("legacy graph → %q, want native", got)
	}
	if got := engineForNodes(nil); got != "native" {
		t.Errorf("empty graph → %q, want native", got)
	}
}
