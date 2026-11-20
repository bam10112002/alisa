package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"alisaai/internal/integrations/ticktick"
	"alisaai/internal/service"
)

type providerStub struct {
	err       error
	projectID string
}

func (p *providerStub) ListProjects(context.Context) ([]ticktick.Project, error) {
	return []ticktick.Project{}, p.err
}

func (p *providerStub) ListProjectTasks(_ context.Context, id string) ([]ticktick.Task, error) {
	p.projectID = id
	return []ticktick.Task{{ID: "task", ProjectID: id, Title: "Example"}}, p.err
}

func TestTickTickRoutes(t *testing.T) {
	provider := &providerStub{}
	router := legacyTestRouter(service.NewTickTick(provider, time.Second), nil)
	for _, path := range []string{"/api/v1/ticktick/projects", "/api/v1/ticktick/tasks", "/api/v1/ticktick/tasks?project_id=work"} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != 200 || !json.Valid(w.Body.Bytes()) || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
	}
	if provider.projectID != "work" {
		t.Fatalf("project filter lost: %q", provider.projectID)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/ticktick/tasks", nil))
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

func TestTickTickErrors(t *testing.T) {
	for _, test := range []struct {
		err    error
		status int
	}{
		{ticktick.ErrNotConfigured, 503},
		{context.DeadlineExceeded, 504},
		{&ticktick.APIError{StatusCode: 401}, 502},
		{&ticktick.APIError{StatusCode: 403}, 502},
		{&ticktick.APIError{StatusCode: 404}, 404},
		{&ticktick.APIError{StatusCode: 429}, 503},
		{errors.New("private provider details"), 502},
	} {
		provider := &providerStub{err: test.err}
		w := httptest.NewRecorder()
		legacyTestRouter(service.NewTickTick(provider, time.Second), nil).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/ticktick/tasks", nil))
		var body map[string]string
		if w.Code != test.status || json.Unmarshal(w.Body.Bytes(), &body) != nil || body["error"] == "" || body["error"] == test.err.Error() {
			t.Fatalf("error %v: status=%d body=%s", test.err, w.Code, w.Body.String())
		}
	}
}

func TestSystemInboxRoute(t *testing.T) {
	provider := &providerStub{}
	router := legacyTestRouter(service.NewTickTick(provider, time.Second), nil)
	w := httptest.NewRecorder()
	// A project filter must not redirect this endpoint to a user-created Inbox.
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/ticktick/inbox/tasks?project_id=custom-inbox", nil))
	var tasks []ticktick.Task
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &tasks) != nil || len(tasks) != 1 || provider.projectID != "inbox" {
		t.Fatalf("unexpected system inbox response: status=%d project=%q body=%s", w.Code, provider.projectID, w.Body.String())
	}
	provider.err = ticktick.ErrNotConfigured
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/ticktick/inbox/tasks", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected configuration error, got %d", w.Code)
	}
}
