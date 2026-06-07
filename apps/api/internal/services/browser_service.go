package services

import (
	"github.com/agently/api/internal/domain"
	"github.com/agently/api/internal/platform"
)

// RecordActionInput is the durable write path's input for a browser action.
type RecordActionInput struct {
	Type       domain.BrowserActionType
	Target     string
	Value      *string
	Status     string
	DurationMs int
}

// BrowserService reads browser sessions and records captured actions.
type BrowserService struct {
	deps Deps
	emit Emitter
}

func NewBrowserService(deps Deps, emit Emitter) *BrowserService {
	return &BrowserService{deps: deps, emit: emit}
}

func (s *BrowserService) assemble(session domain.BrowserSession) domain.BrowserSessionDetail {
	return domain.BrowserSessionDetail{
		BrowserSession: session,
		Shots:          s.deps.Repos.Browser.ListShots(session.ID),
		Actions:        s.deps.Repos.Browser.ListActions(session.ID),
		Console:        s.deps.Repos.Browser.ListConsole(session.ID),
	}
}

func (s *BrowserService) GetByID(id domain.BrowserSessionId) (domain.BrowserSessionDetail, error) {
	session, ok := s.deps.Repos.Browser.GetByID(id)
	if !ok {
		return domain.BrowserSessionDetail{}, domain.NotFound("browser session")
	}
	return s.assemble(session), nil
}

// GetByRun returns the run's browser session, or (zero, false) if none.
func (s *BrowserService) GetByRun(runID domain.RunId) (domain.BrowserSessionDetail, bool) {
	session, ok := s.deps.Repos.Browser.GetByRun(runID)
	if !ok {
		return domain.BrowserSessionDetail{}, false
	}
	return s.assemble(session), true
}

func (s *BrowserService) RecordAction(sessionID domain.BrowserSessionId, input RecordActionInput) (domain.BrowserAction, error) {
	session, ok := s.deps.Repos.Browser.GetByID(sessionID)
	if !ok {
		return domain.BrowserAction{}, domain.NotFound("browser session")
	}
	action := domain.BrowserAction{
		ID: domain.NewBrowserActionId(), SessionID: sessionID, Ts: domain.Timestamp(s.deps.Clock.ISO()),
		Type: input.Type, Target: input.Target, Value: input.Value, Status: input.Status, DurationMs: input.DurationMs,
	}
	s.deps.Repos.Browser.InsertAction(action)
	count := session.ActionsCount + 1
	_, _ = s.deps.Repos.Browser.Update(sessionID, platform.BrowserSessionPatch{ActionsCount: &count})
	s.emit(domain.RunBrowserActionEvent{RunID: session.RunID, SessionID: sessionID, Action: action})
	return action, nil
}
