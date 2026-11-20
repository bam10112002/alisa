package service

import (
	"context"
	"time"

	"alisaai/internal/integrations/ticktick"
)

type TickTickProvider interface {
	ListProjects(context.Context) ([]ticktick.Project, error)
	ListProjectTasks(context.Context, string) ([]ticktick.Task, error)
}

type TickTick struct {
	provider TickTickProvider
	timeout  time.Duration
}

func NewTickTick(provider TickTickProvider, timeout time.Duration) *TickTick {
	return &TickTick{provider: provider, timeout: timeout}
}

func (s *TickTick) ListProjects(ctx context.Context) ([]ticktick.Project, error) {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	return s.provider.ListProjects(ctx)
}

// ListInboxTasks addresses the system inbox by its reserved API identifier,
// never by the display name of a user-created project.
func (s *TickTick) ListInboxTasks(ctx context.Context) ([]ticktick.Task, error) {
	return s.ListTasks(ctx, "inbox")
}

// ListTasks returns project tasks. An empty projectID aggregates all listed projects.
// Any provider failure fails the entire operation rather than returning partial data.
func (s *TickTick) ListTasks(ctx context.Context, projectID string) ([]ticktick.Task, error) {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	if projectID != "" {
		return s.provider.ListProjectTasks(ctx, projectID)
	}
	projects, err := s.provider.ListProjects(ctx)
	if err != nil {
		return nil, err
	}
	tasks := make([]ticktick.Task, 0)
	for _, project := range projects {
		if project.Kind == "NOTE" {
			continue
		}
		projectTasks, err := s.provider.ListProjectTasks(ctx, project.ID)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, projectTasks...)
	}
	return tasks, nil
}
