package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"alisaai/internal/integrations/gigachat"
	"alisaai/internal/integrations/ticktick"
	"alisaai/internal/service"
)

type chatFunc func(context.Context, string) (string, error)

func (f chatFunc) Chat(ctx context.Context, message string) (string, error) { return f(ctx, message) }

func chatRequest(body string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/api/v1/gigachat/chat", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json; charset=utf-8")
	return r
}

func TestChatRequestValidation(t *testing.T) {
	for _, tc := range []struct {
		name, body, contentType, method string
		status                          int
	}{
		{"empty", "", "application/json", "POST", 400},
		{"missing message", `{}`, "application/json", "POST", 400},
		{"blank message", `{"message":"  "}`, "application/json", "POST", 400},
		{"wrong type", `{"message":42}`, "application/json", "POST", 400},
		{"null", `null`, "application/json", "POST", 400},
		{"unknown field", `{"message":"hi","extra":1}`, "application/json", "POST", 400},
		{"trailing object", `{"message":"hi"}{}`, "application/json", "POST", 400},
		{"trailing garbage", `{"message":"hi"}x`, "application/json", "POST", 400},
		{"too large", `{"message":"` + strings.Repeat("x", 64<<10) + `"}`, "application/json", "POST", 413},
		{"wrong content type", `{"message":"hi"}`, "text/plain", "POST", 415},
		{"wrong method", ``, "application/json", "GET", 405},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := chatFunc(func(context.Context, string) (string, error) {
				t.Fatal("invalid request reached service")
				return "", nil
			})
			r := httptest.NewRequest(tc.method, "/api/v1/gigachat/chat", strings.NewReader(tc.body))
			r.Header.Set("Content-Type", tc.contentType)
			w := httptest.NewRecorder()
			legacyTestRouter(nil, model).ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
}

func TestChatResponseAndContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	model := chatFunc(func(got context.Context, message string) (string, error) {
		if message != "Привет" || !errors.Is(got.Err(), context.Canceled) {
			t.Fatal("message or request context lost")
		}
		return "Здравствуйте", nil
	})
	w := httptest.NewRecorder()
	legacyTestRouter(nil, model).ServeHTTP(w, chatRequest(`{"message":"Привет"}`).WithContext(ctx))
	var result map[string]string
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || result["answer"] != "Здравствуйте" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestChatErrors(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
	}{
		{gigachat.ErrNotConfigured, 503}, {ticktick.ErrNotConfigured, 503},
		{context.DeadlineExceeded, 504},
		{&gigachat.APIError{StatusCode: 401}, 502}, {&gigachat.APIError{StatusCode: 429}, 503},
		{&ticktick.APIError{StatusCode: 429}, 503},
		{service.ErrFunctionCallLimit, 502}, {errors.New("private-details"), 502},
	} {
		t.Run(tc.err.Error(), func(t *testing.T) {
			model := chatFunc(func(context.Context, string) (string, error) { return "", fmt.Errorf("wrapped: %w", tc.err) })
			w := httptest.NewRecorder()
			legacyTestRouter(nil, model).ServeHTTP(w, chatRequest(`{"message":"tasks"}`))
			if w.Code != tc.status || !json.Valid(w.Body.Bytes()) || strings.Contains(w.Body.String(), "private-details") {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
}

type functionChatProvider struct{ calls int }

func (p *functionChatProvider) Chat(_ context.Context, r gigachat.ChatRequest) (gigachat.ChatResponse, error) {
	p.calls++
	if p.calls == 1 {
		return gigachat.ChatResponse{Choices: []gigachat.Choice{{Message: gigachat.Message{Role: "assistant", FunctionCall: &gigachat.FunctionCall{Name: "get_ticktick_inbox_tasks", Arguments: json.RawMessage(`{}`)}}, FinishReason: "function_call"}}}, nil
	}
	var result struct {
		Tasks []ticktick.Task `json:"tasks"`
	}
	last := r.Messages[len(r.Messages)-1]
	if last.Role != "function" || json.Unmarshal([]byte(last.Content), &result) != nil || len(result.Tasks) != 1 || result.Tasks[0].ProjectID != "inbox" {
		return gigachat.ChatResponse{}, errors.New("missing inbox tool result")
	}
	return gigachat.ChatResponse{Choices: []gigachat.Choice{{Message: gigachat.Message{Role: "assistant", Content: result.Tasks[0].Title}, FinishReason: "stop"}}}, nil
}

func TestChatRouteRunsInboxTool(t *testing.T) {
	tasksProvider := &providerStub{}
	tasks := service.NewTickTick(tasksProvider, time.Second)
	model := &functionChatProvider{}
	chat := service.NewGigaChat(model, tasks, "", time.Second)
	w := httptest.NewRecorder()
	legacyTestRouter(tasks, chat).ServeHTTP(w, chatRequest(`{"message":"Покажи входящие TickTick"}`))
	if w.Code != 200 || tasksProvider.projectID != "inbox" || model.calls != 2 || !strings.Contains(w.Body.String(), "Example") {
		t.Fatalf("status=%d body=%s modelCalls=%d project=%s", w.Code, w.Body.String(), model.calls, tasksProvider.projectID)
	}
}
