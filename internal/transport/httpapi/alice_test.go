package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"alisaai/internal/integrations/gigachat"
	"alisaai/internal/service"
)

func aliceTestRequest(command string, newSession bool) *http.Request {
	body, _ := json.Marshal(map[string]any{
		"version": "1.0",
		"session": map[string]any{"session_id": "test-session", "skill_id": "test-skill", "message_id": 0, "new": newSession},
		"request": map[string]any{"type": "SimpleUtterance", "command": command, "original_utterance": command, "nlu": map[string]any{"tokens": []string{}}},
		"meta":    map[string]any{"locale": "ru-RU", "timezone": "Europe/Moscow"},
		"state":   map[string]any{"session": map[string]any{}},
	})
	r := httptest.NewRequest(http.MethodPost, "/api/v1/alice/webhook", strings.NewReader(string(body)))
	r.Header.Set("Content-Type", "application/json; charset=utf-8")
	return r
}

type timeZoneChatProvider struct{ systemPrompt string }

func (p *timeZoneChatProvider) Chat(_ context.Context, request gigachat.ChatRequest) (gigachat.ChatResponse, error) {
	p.systemPrompt = request.Messages[0].Content
	return gigachat.ChatResponse{Choices: []gigachat.Choice{{Message: gigachat.Message{Role: "assistant", Content: "Ответ"}, FinishReason: "stop"}}}, nil
}

func TestAlicePassesTimeZoneToSystemPrompt(t *testing.T) {
	p := &timeZoneChatProvider{}
	chat := service.NewGigaChat(p, nil, "", time.Second)
	body := `{"version":"1.0","meta":{"timezone":"Asia/Vladivostok"},"session":{"session_id":"s","skill_id":"test"},"request":{"type":"SimpleUtterance","command":"Сегодня?"}}`
	r := httptest.NewRequest("POST", "/api/v1/alice/webhook", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	NewRouter(chat).ServeHTTP(w, r)
	readAliceResponse(t, w)
	if !strings.Contains(p.systemPrompt, "Часовой пояс пользователя: Asia/Vladivostok") {
		t.Fatal("Alice timezone was not passed to GigaChat")
	}
}

func readAliceResponse(t *testing.T, w *httptest.ResponseRecorder) aliceResponse {
	t.Helper()
	var result aliceResponse
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || result.Version != "1.0" || result.Response.Text == "" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("invalid Alice response: %d %s", w.Code, w.Body.String())
	}
	// false must be serialized, not omitted.
	if !strings.Contains(w.Body.String(), `"end_session":`) {
		t.Fatal("missing end_session")
	}
	return result
}

func TestAliceLocalReplies(t *testing.T) {
	for _, tc := range []struct {
		command         string
		newSession, end bool
		want            string
	}{
		{"", true, false, aliceWelcome}, {"", false, false, aliceWelcome},
		{"помощь", false, false, aliceWelcome}, {"что ты умеешь", false, false, aliceWelcome},
		{"ping", false, false, "Навык работает."}, {"выход", false, true, "До встречи!"},
	} {
		t.Run(tc.command, func(t *testing.T) {
			model := chatFunc(func(context.Context, string) (string, error) { t.Fatal("local response called model"); return "", nil })
			w := httptest.NewRecorder()
			NewRouter(model).ServeHTTP(w, aliceTestRequest(tc.command, tc.newSession))
			result := readAliceResponse(t, w)
			if result.Response.Text != tc.want || result.Response.EndSession != tc.end {
				t.Fatalf("response=%+v", result)
			}
		})
	}
}

func TestAliceCommandAndDeadline(t *testing.T) {
	for _, newSession := range []bool{true, false} {
		model := chatFunc(func(ctx context.Context, message string) (string, error) {
			if message != "мои задачи" {
				t.Fatalf("message=%q", message)
			}
			deadline, ok := ctx.Deadline()
			if !ok || time.Until(deadline) > aliceTimeout || time.Until(deadline) <= 0 {
				t.Fatal("missing Alice deadline")
			}
			return "У вас 11 задач.", nil
		})
		w := httptest.NewRecorder()
		NewRouter(model).ServeHTTP(w, aliceTestRequest("мои задачи", newSession))
		result := readAliceResponse(t, w)
		if result.Response.Text != "У вас 11 задач." || result.Response.EndSession {
			t.Fatalf("response=%+v", result)
		}
	}
}

func TestAliceProtocolSpecialRequests(t *testing.T) {
	for _, tc := range []struct {
		body string
		want string
	}{
		{`{"type":"SimpleUtterance","command":"","original_utterance":"запусти мой навык"}`, aliceWelcome},
		{`{"type":"SimpleUtterance","command":"","original_utterance":"ping"}`, "Навык работает."},
		{`{"type":"ButtonPressed","payload":{"command":"tasks"}}`, "Пожалуйста, задайте вопрос голосом или текстом."},
	} {
		body := `{"version":"1.0","session":{"session_id":"s","skill_id":"skill","new":true},"request":` + tc.body + `}`
		r := httptest.NewRequest("POST", "/api/v1/alice/webhook", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		NewRouter(nil).ServeHTTP(w, r)
		if result := readAliceResponse(t, w); result.Response.Text != tc.want {
			t.Fatalf("response=%+v", result)
		}
	}
}

func TestAliceErrorsAndTextLimit(t *testing.T) {
	for _, tc := range []struct {
		name, answer string
		err          error
		want         string
	}{
		{"provider error", "", errors.New("private-key and private task details"), aliceUnavailable},
		{"timeout", "", context.DeadlineExceeded, aliceTimedOut},
		{"empty answer", "  ", nil, aliceUnavailable},
		{"unicode limit", strings.Repeat("Я", 1100), nil, strings.Repeat("Я", 1023) + "…"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := chatFunc(func(context.Context, string) (string, error) { return tc.answer, tc.err })
			w := httptest.NewRecorder()
			NewRouter(model).ServeHTTP(w, aliceTestRequest("задачи", false))
			result := readAliceResponse(t, w)
			if result.Response.Text != tc.want || result.Response.EndSession || !utf8.ValidString(result.Response.Text) || utf8.RuneCountInString(result.Response.Text) > 1024 {
				t.Fatalf("response=%+v", result)
			}
		})
	}
}

func TestAliceTimeoutCancelsWork(t *testing.T) {
	canceled := false
	model := chatFunc(func(ctx context.Context, _ string) (string, error) {
		<-ctx.Done()
		canceled = true
		return "", ctx.Err()
	})
	w := httptest.NewRecorder()
	aliceHandler{service: model, timeout: 5 * time.Millisecond}.webhook(w, aliceTestRequest("задачи", false))
	if result := readAliceResponse(t, w); result.Response.Text != aliceTimedOut || !canceled {
		t.Fatal("timeout did not cancel provider and produce a reply")
	}
}

func TestAliceParentCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	model := chatFunc(func(ctx context.Context, _ string) (string, error) {
		if !errors.Is(ctx.Err(), context.Canceled) {
			t.Fatal("lost parent cancellation")
		}
		return "", ctx.Err()
	})
	w := httptest.NewRecorder()
	NewRouter(model).ServeHTTP(w, aliceTestRequest("задачи", false).WithContext(ctx))
	readAliceResponse(t, w)
}

func TestAliceValidation(t *testing.T) {
	for _, tc := range []struct {
		name, body, contentType string
		status                  int
	}{
		{"empty", "", "application/json", 400}, {"null", "null", "application/json", 400},
		{"missing fields", `{"version":"1.0"}`, "application/json", 400},
		{"unsupported version", `{"version":"2.0","request":{"type":"SimpleUtterance"},"session":{"session_id":"s","skill_id":"skill"}}`, "application/json", 400},
		{"trailing JSON", `{} {}`, "application/json", 400},
		{"trailing garbage", `{} x`, "application/json", 400},
		{"oversized", `{"command":"` + strings.Repeat("a", 64<<10) + `"}`, "application/json", 413},
		{"content type", `{}`, "text/plain", 415},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := chatFunc(func(context.Context, string) (string, error) { t.Fatal("invalid request called model"); return "", nil })
			r := httptest.NewRequest("POST", "/api/v1/alice/webhook", strings.NewReader(tc.body))
			r.Header.Set("Content-Type", tc.contentType)
			w := httptest.NewRecorder()
			NewRouter(model).ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
}

func TestOnlyAliceRouteIsExposed(t *testing.T) {
	router := NewRouter(nil)
	for _, path := range []string{"/healthz", "/api/v1/ticktick/projects", "/api/v1/ticktick/tasks", "/api/v1/ticktick/inbox/tasks", "/api/v1/gigachat/chat", "/", "/api/v1/alice/webhook/"} {
		for _, method := range []string{"GET", "POST"} {
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest(method, path, nil))
			if w.Code != 404 {
				t.Fatalf("%s %s unexpectedly exposed: %d", method, path, w.Code)
			}
		}
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/alice/webhook", nil))
	if w.Code != 405 {
		t.Fatalf("GET webhook status=%d", w.Code)
	}
}

func TestAliceRunsInboxTool(t *testing.T) {
	provider := &providerStub{}
	tasks := service.NewTickTick(provider, time.Second)
	model := &functionChatProvider{}
	chat := service.NewGigaChat(model, tasks, "", time.Second)
	w := httptest.NewRecorder()
	NewRouter(chat).ServeHTTP(w, aliceTestRequest("мои задачи во входящих", false))
	result := readAliceResponse(t, w)
	if result.Response.Text != "Example" || provider.projectID != "inbox" || model.calls != 2 {
		t.Fatalf("response=%+v project=%s modelCalls=%d", result, provider.projectID, model.calls)
	}
}
