package ticktick

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestListProjectTasks(t *testing.T) {
	client := NewClient("secret-token", time.Second)
	client.httpClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.String() != baseURL+"/project/work/data" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL)
		}
		if r.Header.Get("Authorization") != "Bearer secret-token" || r.Header.Get("Accept") != "application/json" {
			t.Fatal("missing authentication or content negotiation")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"tasks":[{"id":"one","projectId":"work","title":"Task","dueDate":"2026-11-20T12:00:00+0300","items":[{"id":"sub","title":"Step","status":0}]}]}`)), Header: make(http.Header)}, nil
	})
	tasks, err := client.ListProjectTasks(context.Background(), "work")
	if err != nil || len(tasks) != 1 || tasks[0].DueDate != "2026-11-20T12:00:00+0300" || len(tasks[0].Items) != 1 {
		t.Fatalf("unexpected tasks: %+v, error: %v", tasks, err)
	}
}

func TestClientFailures(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
	}{
		{"unauthorized", 401, "private upstream details"},
		{"rate limited", 429, "private upstream details"},
		{"server error", 500, "private upstream details"},
		{"invalid JSON", 200, "invalid"},
		{"oversized", 200, strings.Repeat(" ", maxResponseBytes+1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := NewClient("secret-token", time.Second)
			client.httpClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: test.status, Body: io.NopCloser(strings.NewReader(test.body))}, nil
			})
			_, err := client.ListProjects(context.Background())
			if err == nil || strings.Contains(err.Error(), "secret-token") || strings.Contains(err.Error(), "private upstream details") {
				t.Fatalf("unsafe or missing error: %v", err)
			}
			if test.status != 200 {
				var apiError *APIError
				if !errors.As(err, &apiError) || apiError.StatusCode != test.status {
					t.Fatalf("unexpected API error: %v", err)
				}
			}
		})
	}
}

func TestMissingTokenDoesNotMakeRequest(t *testing.T) {
	client := NewClient(" ", time.Second)
	client.httpClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("request made without token")
		return nil, nil
	})
	if _, err := client.ListProjects(context.Background()); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClientPropagatesCancellation(t *testing.T) {
	client := NewClient("token", time.Second)
	client.httpClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return nil, r.Context().Err()
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.ListProjects(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
}

func TestEmptyTasksAreArray(t *testing.T) {
	client := NewClient("token", time.Second)
	client.httpClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"tasks":null}`))}, nil
	})
	tasks, err := client.ListProjectTasks(context.Background(), "work")
	if err != nil || tasks == nil || len(tasks) != 0 {
		t.Fatalf("expected empty non-nil tasks: %+v, %v", tasks, err)
	}
}
