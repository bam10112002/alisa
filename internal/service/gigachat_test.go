package service

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"alisaai/internal/integrations/gigachat"
	"alisaai/internal/integrations/ticktick"
)

type chatFunc func(context.Context, gigachat.ChatRequest) (gigachat.ChatResponse, error)

func (f chatFunc) Chat(ctx context.Context, r gigachat.ChatRequest) (gigachat.ChatResponse, error) {
	return f(ctx, r)
}

type inboxFunc func(context.Context) ([]ticktick.Task, error)

func (f inboxFunc) ListInboxTasks(ctx context.Context) ([]ticktick.Task, error) { return f(ctx) }

func functionResponse(name, arguments string) gigachat.ChatResponse {
	return gigachat.ChatResponse{Choices: []gigachat.Choice{{
		Message:      gigachat.Message{Role: "assistant", FunctionsStateID: "state-123", FunctionCall: &gigachat.FunctionCall{Name: name, Arguments: json.RawMessage(arguments)}},
		FinishReason: "function_call",
	}}}
}
func textResponse() gigachat.ChatResponse {
	return gigachat.ChatResponse{Choices: []gigachat.Choice{{Message: gigachat.Message{Role: "assistant", Content: "Ваши задачи"}, FinishReason: "stop"}}}
}

func TestGigaChatInboxRoundTrip(t *testing.T) {
	provider := &providerStub{}
	inbox := NewTickTick(provider, time.Second)
	calls := 0
	var deadline time.Time
	model := chatFunc(func(ctx context.Context, request gigachat.ChatRequest) (gigachat.ChatResponse, error) {
		calls++
		d, ok := ctx.Deadline()
		if !ok {
			t.Fatal("missing overall deadline")
		}
		if calls == 1 {
			deadline = d
		} else if !d.Equal(deadline) {
			t.Fatal("deadline was reset")
		}
		if request.Model != "custom-model" || request.FunctionCall != "auto" || len(request.Functions) != 1 || request.Functions[0].Name != inboxFunctionName || request.Stream {
			t.Fatalf("unexpected request: %+v", request)
		}
		if calls == 1 {
			if len(request.Messages) != 2 || request.Messages[1].Content != "Мои входящие" {
				t.Fatalf("unexpected messages: %+v", request.Messages)
			}
			return functionResponse(inboxFunctionName, "{}"), nil
		}
		if len(request.Messages) != 4 {
			t.Fatalf("messages=%+v", request.Messages)
		}
		if !reflect.DeepEqual(request.Messages[2], functionResponse(inboxFunctionName, "{}").Choices[0].Message) {
			t.Fatal("lost function call state")
		}
		result := request.Messages[3]
		if result.Role != "function" || result.Name != inboxFunctionName {
			t.Fatalf("result=%+v", result)
		}
		var data struct {
			Tasks []ticktick.Task `json:"tasks"`
		}
		if err := json.Unmarshal([]byte(result.Content), &data); err != nil || len(data.Tasks) != 1 || data.Tasks[0].ProjectID != "inbox" {
			t.Fatalf("invalid tool result: %s", result.Content)
		}
		return textResponse(), nil
	})
	answer, err := NewGigaChat(model, inbox, "custom-model", time.Second).Chat(context.Background(), "Мои входящие")
	if err != nil || answer != "Ваши задачи" || calls != 2 || !reflect.DeepEqual(provider.calls, []string{"inbox"}) {
		t.Fatalf("answer=%q err=%v modelCalls=%d inboxCalls=%v", answer, err, calls, provider.calls)
	}
}

func TestGigaChatRejectsInvalidCallsAndAnswers(t *testing.T) {
	for _, tc := range []struct {
		name     string
		response gigachat.ChatResponse
	}{
		{"unknown function", functionResponse("delete_task", "{}")},
		{"unexpected argument", functionResponse(inboxFunctionName, `{"project_id":"other"}`)},
		{"null arguments", functionResponse(inboxFunctionName, "null")},
		{"array arguments", functionResponse(inboxFunctionName, "[]")},
		{"string arguments", functionResponse(inboxFunctionName, `"{}"`)},
		{"invalid JSON", functionResponse(inboxFunctionName, "{")},
		{"empty choices", gigachat.ChatResponse{}},
		{"truncated", gigachat.ChatResponse{Choices: []gigachat.Choice{{Message: gigachat.Message{Role: "assistant", Content: "partial"}, FinishReason: "length"}}}},
		{"missing call", gigachat.ChatResponse{Choices: []gigachat.Choice{{Message: gigachat.Message{Role: "assistant"}, FinishReason: "function_call"}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inbox := inboxFunc(func(context.Context) ([]ticktick.Task, error) { t.Fatal("invalid call executed"); return nil, nil })
			model := chatFunc(func(context.Context, gigachat.ChatRequest) (gigachat.ChatResponse, error) { return tc.response, nil })
			if answer, err := NewGigaChat(model, inbox, "", time.Second).Chat(context.Background(), "tasks"); err == nil || answer != "" {
				t.Fatalf("answer=%q err=%v", answer, err)
			}
		})
	}
}

func TestGigaChatEmptyInboxAndCallLimit(t *testing.T) {
	modelCalls, toolCalls := 0, 0
	model := chatFunc(func(_ context.Context, r gigachat.ChatRequest) (gigachat.ChatResponse, error) {
		modelCalls++
		if modelCalls > 1 && r.Messages[len(r.Messages)-1].Content != `{"count":0,"tasks":[]}` {
			t.Fatal("nil tasks must be an empty array")
		}
		return functionResponse(inboxFunctionName, "{}"), nil
	})
	inbox := inboxFunc(func(context.Context) ([]ticktick.Task, error) { toolCalls++; return nil, nil })
	_, err := NewGigaChat(model, inbox, "", time.Second).Chat(context.Background(), "tasks")
	if !errors.Is(err, ErrFunctionCallLimit) || toolCalls != maxFunctionCalls || modelCalls != maxFunctionCalls+1 {
		t.Fatalf("err=%v tool=%d model=%d", err, toolCalls, modelCalls)
	}
}

func TestGigaChatErrorsAndDirectAnswer(t *testing.T) {
	failure := errors.New("provider unavailable")
	for _, tc := range []struct {
		name                   string
		modelError, inboxError error
		direct                 bool
	}{
		{"model failure", failure, nil, false},
		{"inbox failure", nil, failure, false},
		{"direct answer", nil, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			modelCalls, toolCalls := 0, 0
			model := chatFunc(func(context.Context, gigachat.ChatRequest) (gigachat.ChatResponse, error) {
				modelCalls++
				if tc.direct {
					return textResponse(), nil
				}
				return functionResponse(inboxFunctionName, "{}"), tc.modelError
			})
			inbox := inboxFunc(func(context.Context) ([]ticktick.Task, error) { toolCalls++; return nil, tc.inboxError })
			answer, err := NewGigaChat(model, inbox, "", time.Second).Chat(context.Background(), "tasks")
			if tc.direct {
				if err != nil || answer == "" || toolCalls != 0 {
					t.Fatalf("answer=%q err=%v tools=%d", answer, err, toolCalls)
				}
			} else if !errors.Is(err, failure) || answer != "" {
				t.Fatalf("answer=%q err=%v", answer, err)
			}
			if modelCalls != 1 {
				t.Fatalf("model calls=%d", modelCalls)
			}
		})
	}
}

func TestGigaChatCancellationAndEmptyPrompt(t *testing.T) {
	model := chatFunc(func(context.Context, gigachat.ChatRequest) (gigachat.ChatResponse, error) {
		t.Fatal("unexpected provider call")
		return gigachat.ChatResponse{}, nil
	})
	s := NewGigaChat(model, nil, "", time.Second)
	if _, err := s.Chat(context.Background(), "  "); err == nil {
		t.Fatal("empty prompt accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Chat(ctx, "tasks"); !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
}

func TestGigaChatOverallTimeout(t *testing.T) {
	model := chatFunc(func(ctx context.Context, _ gigachat.ChatRequest) (gigachat.ChatResponse, error) {
		<-ctx.Done()
		return gigachat.ChatResponse{}, ctx.Err()
	})
	_, err := NewGigaChat(model, nil, "", 5*time.Millisecond).Chat(context.Background(), "hello")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err=%v", err)
	}
}
