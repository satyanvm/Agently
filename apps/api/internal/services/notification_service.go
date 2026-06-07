package services

import (
	"github.com/agently/api/internal/domain"
	"github.com/agently/api/internal/domain/validate"
	"github.com/agently/api/internal/platform"
)

// CreateNotificationInput is the durable write path's input for a notification.
type CreateNotificationInput struct {
	Type         domain.NotificationType
	Severity     domain.Severity
	Title        string
	Body         string
	WorkflowSlug *string
	RunID        *domain.RunId
	RunNumber    *int
}

// NotificationService lists notifications and marks them read.
type NotificationService struct {
	deps Deps
	emit Emitter
}

func NewNotificationService(deps Deps, emit Emitter) *NotificationService {
	return &NotificationService{deps: deps, emit: emit}
}

func (s *NotificationService) List(query validate.ListNotificationsQuery) domain.Page[domain.Notification] {
	items := s.deps.Repos.Notifications.All()
	platform.SortByNewest(items, func(n domain.Notification) string { return string(n.CreatedAt) })
	filtered := items[:0:0]
	for _, n := range items {
		if query.UnreadSet && *query.Unread && n.ReadAt != nil {
			continue
		}
		if query.Type != "" && string(n.Type) != query.Type {
			continue
		}
		filtered = append(filtered, n)
	}
	return platform.Paginate(filtered, query.PageQuery)
}

func (s *NotificationService) MarkRead(id domain.NotificationId) (domain.Notification, error) {
	existing, ok := s.deps.Repos.Notifications.GetByID(id)
	if !ok {
		return domain.Notification{}, domain.NotFound("notification")
	}
	if existing.ReadAt != nil {
		return existing, nil
	}
	at := domain.Timestamp(s.deps.Clock.ISO())
	return s.deps.Repos.Notifications.Update(id, platform.NotificationPatch{ReadAt: &at})
}

func (s *NotificationService) MarkAllRead() int {
	at := domain.Timestamp(s.deps.Clock.ISO())
	return s.deps.Repos.Notifications.MarkAllRead(s.deps.Repos.Workspace.Get().ID, at)
}

func (s *NotificationService) Create(input CreateNotificationInput) domain.Notification {
	var recipient *domain.MemberId
	if members := s.deps.Repos.Members.All(); len(members) > 0 {
		recipient = &members[0].ID
	}
	n := domain.Notification{
		ID: domain.NewNotificationId(), WorkspaceID: s.deps.Repos.Workspace.Get().ID, RecipientID: recipient,
		Type: input.Type, Severity: input.Severity, Title: input.Title, Body: input.Body,
		WorkflowSlug: input.WorkflowSlug, RunID: input.RunID, RunNumber: input.RunNumber,
		ReadAt: nil, CreatedAt: domain.Timestamp(s.deps.Clock.ISO()),
	}
	s.deps.Repos.Notifications.Insert(n)
	s.emit(domain.NotificationCreatedEvent{NotificationID: n.ID})
	return n
}
