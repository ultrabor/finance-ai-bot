// Package llm — порт агента к языковой модели: формат сообщений Chat Completions с tool calling
// и интерфейс клиента. Реализация — internal/adapter/llm/openai.
package llm

import (
	"context"
	"encoding/json"
)

const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleTool      = "tool"
)

type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// MarshalJSON отправляет content: null для ответа ассистента с вызовами инструментов —
// так требует спецификация OpenAI; часть совместимых провайдеров отклоняет пустую строку.
func (m Message) MarshalJSON() ([]byte, error) {
	type wire Message
	out := struct {
		wire
		Content *string `json:"content"`
	}{wire: wire(m)}
	if m.Content != "" || len(m.ToolCalls) == 0 {
		out.Content = &m.Content
	}
	return json.Marshal(out)
}

type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

type FunctionCall struct {
	Name string `json:"name"`
	// Arguments — JSON-объект аргументов в виде строки (так его отдаёт API).
	Arguments string `json:"arguments"`
}

type Tool struct {
	Type     string      `json:"type"`
	Function FunctionDef `json:"function"`
}

type FunctionDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// Client — абстракция над провайдером LLM; в тестах подменяется фейком.
type Client interface {
	Chat(ctx context.Context, messages []Message, tools []Tool) (Message, error)
}
