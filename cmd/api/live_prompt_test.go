package main

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"alisaai/internal/config"
	"alisaai/internal/integrations/gigachat"
	"alisaai/internal/integrations/ticktick"
	"alisaai/internal/service"
)

type promptInbox struct {
	tasks []ticktick.Task
	calls int
}

func (p *promptInbox) ListInboxTasks(context.Context) ([]ticktick.Task, error) {
	p.calls++
	return p.tasks, nil
}

// Opt-in behavioral evaluation: real model, synthetic tasks with known dates.
func TestGigaChatPromptLive(t *testing.T) {
	if os.Getenv("ALISAAI_LIVE_TEST") != "1" {
		t.Skip("set ALISAAI_LIVE_TEST=1 to evaluate the real model")
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
	location, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().In(location)
	date := func(offset int) string {
		d := now.AddDate(0, 0, offset)
		return time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, location).UTC().Format("2006-01-02T15:04:05.000-0700")
	}
	inbox := &promptInbox{tasks: []ticktick.Task{
		{ID: "today", Title: "Забрать атлас", Status: 0, DueDate: date(0), TimeZone: "Europe/Moscow", IsAllDay: true},
		{ID: "overdue", Title: "Оплатить абонемент", Status: 0, DueDate: date(-1), TimeZone: "Europe/Moscow", IsAllDay: true},
		{ID: "future", Title: "Посетить планетарий", Status: 0, DueDate: date(1), TimeZone: "Europe/Moscow", IsAllDay: true},
		{ID: "undated", Title: "Купить телескоп", Status: 0},
		{ID: "completed", Title: "Забрать микроскоп", Status: 2, DueDate: date(0), CompletedTime: date(0), TimeZone: "Europe/Moscow", IsAllDay: true},
	}}
	chat := service.NewGigaChat(client, inbox, cfg.GigaChatModel, cfg.GigaChatTimeout)
	for _, tc := range []struct {
		name, prompt, want string
		excluded           []string
	}{
		{"today", "Что мне нужно выполнить сегодня?", "атлас", []string{"планетарий", "микроскоп"}},
		{"later", "Что мне нужно выполнить позже? Назови только задачи с будущей датой.", "планетарий", []string{"атлас", "телескоп", "микроскоп", "абонемент"}},
		{"follow-up", "Это точно всё нужно выполнить сегодня?", "атлас", []string{"планетарий", "микроскоп"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := inbox.calls
			ctx, cancel := context.WithTimeout(service.WithUserTimeZone(context.Background(), "Europe/Moscow"), 3500*time.Millisecond)
			defer cancel()
			start := time.Now()
			answer, err := chat.Chat(ctx, tc.prompt)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("duration=%s answer=%s", time.Since(start).Round(time.Millisecond), answer)
			lower := strings.ToLower(answer)
			if inbox.calls == before || !strings.Contains(lower, tc.want) {
				t.Fatal("missing tool call or expected task")
			}
			for _, excluded := range tc.excluded {
				if strings.Contains(lower, excluded) {
					t.Errorf("unexpected task %q in answer", excluded)
				}
			}
			if strings.Contains(lower, "абонемент") && !strings.Contains(lower, "просроч") {
				t.Error("overdue task presented without an overdue label")
			}
		})
	}
}
