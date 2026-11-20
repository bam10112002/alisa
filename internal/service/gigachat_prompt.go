package service

import (
	"context"
	_ "embed"
	"fmt"
	"strings"
	"time"
	_ "time/tzdata"
)

//go:embed gigachat_prompt.txt
var gigaChatSystemPrompt string

type userTimeZoneKey struct{}

// WithUserTimeZone carries Alice's request-scoped IANA timezone to the model.
// Missing or invalid zones fall back to the owner's timezone, Europe/Moscow.
func WithUserTimeZone(ctx context.Context, name string) context.Context {
	return context.WithValue(ctx, userTimeZoneKey{}, userLocation(name))
}

func userLocation(name string) *time.Location {
	name = strings.TrimSpace(name)
	if name != "" && name != "Local" {
		if location, err := time.LoadLocation(name); err == nil {
			return location
		}
	}
	location, _ := time.LoadLocation("Europe/Moscow") // Embedded tzdata makes this available on every host.
	return location
}

func gigaChatPrompt(ctx context.Context, now time.Time) string {
	location := locationFromContext(ctx)
	local := now.In(location)
	return fmt.Sprintf("Контекст времени от сервера:\nТекущее время: %s.\nСегодня: %s.\nВчера: %s.\nЗавтра: %s.\nЧасовой пояс пользователя: %s.\n\n%s", local.Format(time.RFC3339), local.Format("2006-01-02"), local.AddDate(0, 0, -1).Format("2006-01-02"), local.AddDate(0, 0, 1).Format("2006-01-02"), location.String(), gigaChatSystemPrompt)
}

func locationFromContext(ctx context.Context) *time.Location {
	if location, ok := ctx.Value(userTimeZoneKey{}).(*time.Location); ok {
		return location
	}
	return userLocation("")
}
