package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"alisaai/internal/integrations/gigachat"
)

func TestGigaChatUsesRequestTimeZoneAndFreshDate(t *testing.T) {
	for _, tc := range []struct{ zone, wantZone, wantDate string }{
		{"Europe/Moscow", "Europe/Moscow", "2026-11-21"},
		{"America/New_York", "America/New_York", "2026-11-20"},
		{"", "Europe/Moscow", "2026-11-21"},
		{"invalid/timezone", "Europe/Moscow", "2026-11-21"},
		{"Local", "Europe/Moscow", "2026-11-21"},
	} {
		t.Run(tc.zone, func(t *testing.T) {
			var requests []gigachat.ChatRequest
			model := chatFunc(func(_ context.Context, r gigachat.ChatRequest) (gigachat.ChatResponse, error) {
				requests = append(requests, r)
				return textResponse(), nil
			})
			s := NewGigaChat(model, nil, "", time.Second)
			now := time.Date(2026, 11, 20, 23, 30, 0, 0, time.UTC)
			s.now = func() time.Time { return now }
			ctx := WithUserTimeZone(context.Background(), tc.zone)
			if _, err := s.Chat(ctx, "Сегодня?"); err != nil {
				t.Fatal(err)
			}
			prompt := requests[0].Messages[0]
			if prompt.Role != "system" || !strings.Contains(prompt.Content, "Сегодня: "+tc.wantDate) || !strings.Contains(prompt.Content, "Часовой пояс пользователя: "+tc.wantZone) {
				t.Fatalf("incorrect time context for %q", tc.zone)
			}
			now = now.AddDate(0, 0, 1)
			if _, err := s.Chat(ctx, "Сегодня?"); err != nil {
				t.Fatal(err)
			}
			if requests[1].Messages[0].Content == prompt.Content {
				t.Fatal("date was cached across requests")
			}
			if len(requests[1].Messages) != 2 {
				t.Fatal("history leaked into a new request")
			}
		})
	}
}
