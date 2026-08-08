package services

import (
	"net/http"
	"time"

	"github.com/agently/api/internal/domain"
	"github.com/agently/api/internal/platform"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Platform assembles repositories, the event bus, services, a clock, and a
// logger into a single object the HTTP layer depends on. Mirrors the Platform
// type + createPlatform factory in packages/core/src/platform/index.ts.
type Platform struct {
	Repos       *platform.Repositories
	Bus         platform.EventBus
	Clock       platform.Clock
	Logger      platform.Logger
	WorkspaceID domain.WorkspaceId

	Workflows     *WorkflowService
	Agents        *AgentService
	Runs          *RunService
	Logs          *LogService
	Browser       *BrowserService
	Notifications *NotificationService
	Activity      *ActivityService
	Stats         *StatsService
	Integrations  *IntegrationService
	Credentials   *CredentialService
}

// Options configure platform construction. Tests may provide in-memory
// repositories explicitly; the server provides a Postgres pool.
//
// Storage selection: if Pool is non-nil, the platform uses Postgres-backed
// repositories (seeding the DB from the canonical dataset on first boot).
// Explicit Repositories, if set, are intended for tests.
type Options struct {
	Clock        platform.Clock
	Logger       platform.Logger
	Bus          platform.EventBus
	Repositories *platform.Repositories
	Seed         *platform.Seed
	Pool         *pgxpool.Pool
}

// NewPlatform wires the platform together.
func NewPlatform(opts Options) *Platform {
	clock := opts.Clock
	if clock == nil {
		clock = platform.SystemClock
	}
	logger := opts.Logger
	if logger == nil {
		logger = platform.NewLogger(true, "svc", "platform")
	}
	bus := opts.Bus
	if bus == nil {
		bus = platform.NewEventBus()
	}
	var seed platform.Seed
	if opts.Seed != nil {
		seed = *opts.Seed
	} else {
		seed = platform.BuildSeed(clock)
	}
	repos := opts.Repositories
	if repos == nil {
		if opts.Pool != nil {
			repos = platform.NewPostgresRepositories(opts.Pool, logger)
			if err := platform.SeedPostgresIfEmpty(repos, opts.Pool, seed.Data, logger); err != nil {
				logger.Error("postgres seed failed", "error", err.Error())
			}
		} else {
			repos = platform.NewMemoryRepositories(seed.Data)
		}
	}

	workspaceID := repos.Workspace.Get().ID
	deps := Deps{
		Repos:  repos,
		Bus:    bus,
		Clock:  clock,
		Logger: logger,
		SeedStats: SeedStats{
			WorkflowStats:  seed.WorkflowStats,
			WorkspaceStats: seed.WorkspaceStats,
		},
	}
	emit := NewEmitter(bus, clock, workspaceID)

	logs := NewLogService(deps, emit)

	return &Platform{
		Repos:         repos,
		Bus:           bus,
		Clock:         clock,
		Logger:        logger,
		WorkspaceID:   workspaceID,
		Workflows:     NewWorkflowService(deps),
		Agents:        NewAgentService(deps),
		Runs:          NewRunService(deps, emit, logs),
		Logs:          logs,
		Browser:       NewBrowserService(deps, emit),
		Notifications: NewNotificationService(deps, emit),
		Activity:      NewActivityService(deps),
		Stats:         NewStatsService(deps),
		Integrations:  NewIntegrationService(deps, &http.Client{Timeout: 20 * time.Second}),
		Credentials:   NewCredentialService(deps),
	}
}
