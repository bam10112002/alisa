package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"

	"alisaai/internal/config"
	"alisaai/internal/integrations/gigachat"
	"alisaai/internal/integrations/ticktick"
	"alisaai/internal/service"
	"alisaai/internal/transport/httpapi"
)

type observedInbox struct {
	service      *service.TickTick
	calls, count int
}

func (p *observedInbox) ListInboxTasks(ctx context.Context) ([]ticktick.Task, error) {
	p.calls++
	tasks, err := p.service.ListInboxTasks(ctx)
	p.count = len(tasks)
	return tasks, err
}

type observedChat struct {
	service *service.GigaChat
	err     error
}

func (p *observedChat) Chat(ctx context.Context, message string) (string, error) {
	answer, err := p.service.Chat(ctx, message)
	p.err = err
	return answer, err
}

// Opt-in: uses configured credentials, sends Inbox tasks to GigaChat and consumes API quota.
func TestAliceLive(t *testing.T) {
	if os.Getenv("ALISAAI_LIVE_TEST") != "1" {
		t.Skip("set ALISAAI_LIVE_TEST=1 to call real providers")
	}
	t.Chdir("../..")
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	client, err := gigachat.NewClient(gigachat.Options{AuthKey: cfg.GigaChatAuthKey, Scope: cfg.GigaChatScope, BaseURL: cfg.GigaChatBaseURL, AuthURL: cfg.GigaChatAuthURL, CAFile: cfg.GigaChatCAFile, Timeout: cfg.GigaChatTimeout})
	if err != nil {
		t.Fatal(err)
	}
	tasks := service.NewTickTick(ticktick.NewClient(cfg.TickTickToken, cfg.TickTickTimeout), cfg.TickTickTimeout)
	inbox := &observedInbox{service: tasks}
	chat := &observedChat{service: service.NewGigaChat(client, inbox, cfg.GigaChatModel, cfg.GigaChatTimeout)}
	r := httptest.NewRequest(http.MethodPost, "/api/v1/alice/webhook", strings.NewReader(`{"version":"1.0","session":{"session_id":"live-test","skill_id":"local-test","new":false},"request":{"type":"SimpleUtterance","command":"Вызови get_ticktick_inbox_tasks и скажи, сколько всего задач вернул инструмент из моего системного Inbox TickTick. Используй точное значение count, без фильтрации. Ответь только целым числом."}}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	httpapi.NewRouter(chat).ServeHTTP(w, r)
	if w.Code != http.StatusOK || chat.err != nil {
		t.Fatalf("HTTP %d; provider error: %v", w.Code, chat.err)
	}
	var response struct {
		Response struct {
			Text string `json:"text"`
		} `json:"response"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || response.Response.Text == "" {
		t.Fatal("missing answer")
	}
	if inbox.calls == 0 {
		t.Fatal("model did not call Inbox tool")
	}
	if strings.TrimSpace(response.Response.Text) != strconv.Itoa(inbox.count) {
		t.Fatalf("model count does not match Inbox: actual=%d answer=%q", inbox.count, response.Response.Text)
	}
	t.Logf("HTTP %d; Inbox tool calls=%d; actual tasks=%d; answer=%s", w.Code, inbox.calls, inbox.count, response.Response.Text)
}
