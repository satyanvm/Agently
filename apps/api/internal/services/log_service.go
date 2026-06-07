package services

import (
	"strings"
	"time"

	"github.com/agently/api/internal/domain"
	"github.com/agently/api/internal/domain/validate"
	"github.com/agently/api/internal/platform"
)

// AppendLogInput is the durable write path's input for a single log line.
type AppendLogInput struct {
	Level     domain.LogLevel
	Channel   domain.LogChannel
	Source    string
	Message   string
	Detail    *string
	Reasoning *bool
}

// LogService is the durable log write/read path a real runner will call.
type LogService struct {
	deps Deps
	emit Emitter
}

func NewLogService(deps Deps, emit Emitter) *LogService {
	return &LogService{deps: deps, emit: emit}
}

var severityRank = map[domain.LogLevel]int{
	domain.LevelDebug: 0, domain.LevelInfo: 1, domain.LevelSuccess: 1,
	domain.LevelWarn: 2, domain.LevelError: 3,
}

// Append assigns seq + offset, persists, and emits an event.
func (s *LogService) Append(runID domain.RunId, input AppendLogInput) domain.LogEntry {
	run, ok := s.deps.Repos.Runs.GetByID(runID)
	startMs := s.deps.Clock.Now()
	if ok && run.StartedAt != nil {
		if t, err := time.Parse(time.RFC3339, string(*run.StartedAt)); err == nil {
			startMs = t.UnixMilli()
		}
	}
	seq := s.deps.Repos.Logs.MaxSeq(runID) + 1
	offsetMs := s.deps.Clock.Now() - startMs
	if offsetMs < 0 {
		offsetMs = 0
	}
	reasoning := input.Channel == domain.ChannelModel
	if input.Reasoning != nil {
		reasoning = *input.Reasoning
	}
	entry := domain.LogEntry{
		ID: domain.NewLogId(), RunID: runID, Seq: seq, Ts: domain.Timestamp(s.deps.Clock.ISO()),
		OffsetMs: int(offsetMs), Level: input.Level, Channel: input.Channel, Source: input.Source,
		Message: input.Message, Detail: input.Detail, Reasoning: reasoning,
	}
	s.deps.Repos.Logs.Insert(entry)
	s.emit(domain.RunLogAppendedEvent{RunID: runID, Log: entry})
	return entry
}

// List returns filtered, paginated logs for a run.
func (s *LogService) List(runID domain.RunId, query validate.ListLogsQuery) domain.Page[domain.LogEntry] {
	items := s.deps.Repos.Logs.ListByRun(runID)
	filtered := items[:0:0]
	for _, l := range items {
		if query.AfterSeq != nil && l.Seq <= *query.AfterSeq {
			continue
		}
		switch query.Severity {
		case "warn":
			if severityRank[l.Level] < 2 {
				continue
			}
		case "error":
			if l.Level != domain.LevelError {
				continue
			}
		}
		if query.Channel != "" && string(l.Channel) != query.Channel {
			continue
		}
		if query.Source != "" && l.Source != query.Source {
			continue
		}
		if query.Q != "" {
			needle := strings.ToLower(query.Q)
			if !strings.Contains(strings.ToLower(l.Message), needle) &&
				!strings.Contains(strings.ToLower(l.Source), needle) {
				continue
			}
		}
		filtered = append(filtered, l)
	}
	return platform.Paginate(filtered, query.PageQuery)
}
