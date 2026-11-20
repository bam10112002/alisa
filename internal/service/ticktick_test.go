package service

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"alisaai/internal/integrations/ticktick"
)

type providerStub struct {
	calls []string
	fail  bool
}

func (p *providerStub) ListProjects(context.Context) ([]ticktick.Project, error) {
	p.calls = append(p.calls, "projects")
	return []ticktick.Project{{ID: "a", Kind: "TASK"}, {ID: "notes", Kind: "NOTE"}, {ID: "b"}}, nil
}

func (p *providerStub) ListProjectTasks(ctx context.Context, id string) ([]ticktick.Task, error) {
	p.calls = append(p.calls, id)
	if _, ok := ctx.Deadline(); !ok {
		return nil, errors.New("missing overall deadline")
	}
	if p.fail && id == "b" {
		return nil, errors.New("provider unavailable")
	}
	return []ticktick.Task{{ID: id, ProjectID: id}}, nil
}

func TestListTasks(t *testing.T) {
	for _, test := range []struct {
		name      string
		projectID string
		fail      bool
		calls     []string
		count     int
	}{
		{"aggregate", "", false, []string{"projects", "a", "b"}, 2},
		{"selected project", "a", false, []string{"a"}, 1},
		{"no partial data", "", true, []string{"projects", "a", "b"}, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := &providerStub{fail: test.fail}
			tasks, err := NewTickTick(provider, time.Second).ListTasks(context.Background(), test.projectID)
			if (err != nil) != test.fail || len(tasks) != test.count || !reflect.DeepEqual(provider.calls, test.calls) {
				t.Fatalf("tasks=%v err=%v calls=%v", tasks, err, provider.calls)
			}
		})
	}
}
