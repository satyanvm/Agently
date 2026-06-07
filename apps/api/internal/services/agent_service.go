package services

import (
	"github.com/agently/api/internal/domain"
	"github.com/agently/api/internal/domain/validate"
	"github.com/agently/api/internal/platform"
)

// AgentService lists and creates reusable agent definitions.
type AgentService struct {
	deps Deps
}

func NewAgentService(deps Deps) *AgentService {
	return &AgentService{deps: deps}
}

func (s *AgentService) List(query domain.PageQuery) domain.Page[domain.AgentDefinition] {
	return platform.Paginate(s.deps.Repos.Agents.All(), query)
}

func (s *AgentService) Create(input validate.CreateAgentInput) (domain.AgentDefinition, error) {
	now := domain.Timestamp(s.deps.Clock.ISO())
	agent := domain.AgentDefinition{
		ID: domain.NewAgentDefinitionId(), WorkspaceID: s.deps.Repos.Workspace.Get().ID,
		Name: input.Name, Role: input.Role, Model: input.Model, Description: input.Description,
		Tools: input.Tools, Config: input.Config, CreatedAt: now, UpdatedAt: now,
	}
	return s.deps.Repos.Agents.Insert(agent), nil
}
