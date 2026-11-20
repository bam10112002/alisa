package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"alisaai/internal/integrations/gigachat"
	"alisaai/internal/integrations/ticktick"
)

const inboxFunctionName = "get_ticktick_inbox_tasks"
const maxFunctionCalls = 3

var ErrFunctionCallLimit = errors.New("GigaChat function call limit exceeded")

type GigaChatProvider interface {
	Chat(context.Context, gigachat.ChatRequest) (gigachat.ChatResponse, error)
}

type InboxTaskProvider interface {
	ListInboxTasks(context.Context) ([]ticktick.Task, error)
}

type GigaChat struct {
	provider GigaChatProvider
	inbox    InboxTaskProvider
	model    string
	timeout  time.Duration
	now      func() time.Time
}

func NewGigaChat(provider GigaChatProvider, inbox InboxTaskProvider, model string, timeout time.Duration) *GigaChat {
	if strings.TrimSpace(model) == "" {
		model = gigachat.DefaultModel
	}
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	return &GigaChat{provider: provider, inbox: inbox, model: model, timeout: timeout, now: time.Now}
}

// Chat performs one independent conversation, including any Inbox function calls.
// No user history is shared or persisted between calls.
func (s *GigaChat) Chat(ctx context.Context, prompt string) (string, error) {
	if strings.TrimSpace(prompt) == "" {
		return "", errors.New("GigaChat prompt must not be empty")
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	now := s.now()
	request := gigachat.ChatRequest{
		Model: s.model, FunctionCall: "auto",
		Messages: []gigachat.Message{{Role: "system", Content: gigaChatPrompt(ctx, now)}, {Role: "user", Content: prompt}},
		Functions: []gigachat.Function{{
			Name:        inboxFunctionName,
			Description: "Получить текущие задачи пользователя из системного списка Входящие (Inbox) TickTick. Без параметров; только чтение.",
			Parameters:  json.RawMessage(`{"type":"object","properties":{},"required":[]}`),
		}},
	}
	for calls := 0; ; calls++ {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		response, err := s.provider.Chat(ctx, request)
		if err != nil {
			return "", fmt.Errorf("chat with GigaChat: %w", err)
		}
		if len(response.Choices) != 1 {
			return "", errors.New("GigaChat must return one choice")
		}
		choice := response.Choices[0]
		if choice.Message.Role != "assistant" {
			return "", errors.New("GigaChat returned an unexpected message role")
		}
		if choice.FinishReason == "stop" && choice.Message.FunctionCall == nil {
			if strings.TrimSpace(choice.Message.Content) == "" {
				return "", errors.New("GigaChat returned an empty answer")
			}
			return choice.Message.Content, nil
		}
		if choice.FinishReason != "function_call" || choice.Message.FunctionCall == nil {
			return "", errors.New("GigaChat did not complete the answer or return a valid function call")
		}
		if calls >= maxFunctionCalls {
			return "", ErrFunctionCallLimit
		}
		call := choice.Message.FunctionCall
		if call.Name != inboxFunctionName {
			return "", errors.New("GigaChat requested an unknown function")
		}
		var arguments map[string]json.RawMessage
		if err := json.Unmarshal(call.Arguments, &arguments); err != nil || arguments == nil || len(arguments) != 0 {
			return "", errors.New("Inbox function requires an empty arguments object")
		}
		if err := ctx.Err(); err != nil {
			return "", err
		}
		tasks, err := s.inbox.ListInboxTasks(ctx)
		if err != nil {
			return "", fmt.Errorf("get TickTick inbox tasks: %w", err)
		}
		if tasks == nil {
			tasks = []ticktick.Task{}
		}
		result, err := json.Marshal(struct {
			Count int            `json:"count"`
			Tasks []gigaChatTask `json:"tasks"`
		}{Count: len(tasks), Tasks: tasksWithDateHints(tasks, now, locationFromContext(ctx))})
		if err != nil {
			return "", fmt.Errorf("encode Inbox tasks: %w", err)
		}
		// Preserve function_call and functions_state_id exactly as returned by GigaChat.
		request.Messages = append(request.Messages, choice.Message, gigachat.Message{Role: "function", Name: call.Name, Content: string(result)})
	}
}
