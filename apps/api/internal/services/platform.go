package services

import (
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
}

// Options configure platform construction. All fields are optional; sensible
// defaults (system clock, dev logger, in-memory seeded store) are used when nil.
//
// Storage selection: if Pool is non-nil, the platform uses Postgres-backed
// repositories (seeding the DB from the canonical dataset on first boot).
// Otherwise it falls back to the in-memory store. Explicit Repositories, if set,
// override both. Tests pass neither and get the in-memory store.
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
		seed = platform.BuildSeed(platform.FixedClock(platform.NowISO))
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
	}
}
