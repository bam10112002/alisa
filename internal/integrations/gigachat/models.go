package gigachat

import "encoding/json"

type Message struct {
	Role             string        `json:"role"`
	Content          string        `json:"content"`
	Name             string        `json:"name,omitempty"`
	FunctionCall     *FunctionCall `json:"function_call,omitempty"`
	FunctionsStateID string        `json:"functions_state_id,omitempty"`
}

type FunctionCall struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type Function struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type ChatRequest struct {
	Model        string     `json:"model"`
	Messages     []Message  `json:"messages"`
	Functions    []Function `json:"functions,omitempty"`
	FunctionCall string     `json:"function_call,omitempty"`
	Stream       bool       `json:"stream"`
}

type Choice struct {
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

type ChatResponse struct {
	Choices []Choice `json:"choices"`
}
