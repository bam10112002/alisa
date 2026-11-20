package service

import (
	"reflect"
	"testing"
	"time"

	"alisaai/internal/integrations/ticktick"
)

func TestTaskDateHints(t *testing.T) {
	now := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	moscow := userLocation("Europe/Moscow")
	for _, tc := range []struct{ name, raw, wantDate, wantRelation string }{
		{"UTC midnight shift", "2026-11-19T21:00:00.000+0000", "2026-11-20", "сегодня"},
		{"past", "2026-11-18T21:00:00.000+0000", "2026-11-19", "раньше сегодня"},
		{"future", "2026-11-20T21:00:00.000+0000", "2026-11-21", "после сегодня"},
		{"RFC3339", "2026-11-20T00:00:00+03:00", "2026-11-20", "сегодня"},
		{"date only", "2026-11-20", "2026-11-20", "сегодня"},
		{"missing", "", "", "не указана"},
		{"invalid", "not-a-date", "", "не распознана"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := []ticktick.Task{{ID: "t", Title: "unchanged", DueDate: tc.raw, Status: 2}}
			got := tasksWithDateHints(input, now, moscow)
			if len(got) != 1 || !reflect.DeepEqual(got[0].Task, input[0]) {
				t.Fatal("original task was filtered or modified")
			}
			if hint := got[0].DateHints; hint.Due.Date != tc.wantDate || hint.Due.Relation != tc.wantRelation || hint.Start.Relation != "не указана" || hint.Today != "2026-11-20" || hint.TimeZone != "Europe/Moscow" {
				t.Fatalf("hints=%+v", hint)
			}
		})
	}
}

func TestAllDayTaskUsesTaskTimeZone(t *testing.T) {
	now := time.Date(2026, 11, 20, 22, 0, 0, 0, time.UTC)
	input := []ticktick.Task{{StartDate: "2026-11-20T00:00:00-08:00", TimeZone: "America/Los_Angeles", IsAllDay: true}}
	got := tasksWithDateHints(input, now, userLocation("Europe/Moscow"))[0].DateHints
	if got.TimeZone != "America/Los_Angeles" || got.Today != "2026-11-20" || got.Start.Date != "2026-11-20" || got.Start.Relation != "сегодня" {
		t.Fatalf("hints=%+v", got)
	}
	input[0].IsAllDay = false
	got = tasksWithDateHints(input, now, userLocation("Europe/Moscow"))[0].DateHints
	if got.TimeZone != "Europe/Moscow" || got.Today != "2026-11-21" || got.Start.Relation != "раньше сегодня" {
		t.Fatalf("hints=%+v", got)
	}
}
