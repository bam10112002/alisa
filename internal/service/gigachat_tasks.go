package service

import (
	"time"

	"alisaai/internal/integrations/ticktick"
)

// Date hints accompany the unchanged task fields so the model need not perform
// timezone arithmetic. Tasks are not filtered out of the tool result.
type gigaChatTask struct {
	ticktick.Task
	DateHints taskDateHints `json:"dateHints"`
}

type taskDateHints struct {
	TimeZone string   `json:"timeZone"`
	Today    string   `json:"today"`
	Start    dateHint `json:"start"`
	Due      dateHint `json:"due"`
}

type dateHint struct {
	Date     string `json:"date,omitempty"`
	Relation string `json:"relation"`
}

func tasksWithDateHints(tasks []ticktick.Task, now time.Time, userZone *time.Location) []gigaChatTask {
	result := make([]gigaChatTask, 0, len(tasks))
	for _, task := range tasks {
		location := userZone
		if task.IsAllDay && task.TimeZone != "" && task.TimeZone != "Local" {
			if zone, err := time.LoadLocation(task.TimeZone); err == nil {
				location = zone
			}
		}
		today := now.In(location).Format(time.DateOnly)
		result = append(result, gigaChatTask{Task: task, DateHints: taskDateHints{
			TimeZone: location.String(), Today: today,
			Start: makeDateHint(task.StartDate, today, location), Due: makeDateHint(task.DueDate, today, location),
		}})
	}
	return result
}

func makeDateHint(raw, today string, location *time.Location) dateHint {
	if raw == "" {
		return dateHint{Relation: "не указана"}
	}
	var value time.Time
	var err error
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.999999999Z0700", time.DateOnly} {
		value, err = time.ParseInLocation(layout, raw, location)
		if err == nil {
			break
		}
	}
	if err != nil {
		return dateHint{Relation: "не распознана"}
	}
	date := value.In(location).Format(time.DateOnly)
	relation := "сегодня"
	if date < today {
		relation = "раньше сегодня"
	} else if date > today {
		relation = "после сегодня"
	}
	return dateHint{Date: date, Relation: relation}
}
