package services

import (
	"github.com/agently/api/internal/domain"
	"github.com/agently/api/internal/platform"
)

// ActivityService returns the recent workspace activity feed.
type ActivityService struct {
	deps Deps
}

func NewActivityService(deps Deps) *ActivityService {
	return &ActivityService{deps: deps}
}

func (s *ActivityService) List(query domain.PageQuery) domain.Page[domain.ActivityEvent] {
	items := s.deps.Repos.Activity.All()
	platform.SortByNewest(items, func(a domain.ActivityEvent) string { return string(a.At) })
	return platform.Paginate(items, query)
}
