package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

const maxAttempts = 3

// OpenAIClient вызывает POST {BaseURL}/chat/completions.
type OpenAIClient struct {
	baseURL     string
	apiKey      string
	model       string
	temperature *float64
	http        *http.Client
}

type OpenAIConfig struct {
	BaseURL     string
	APIKey      string
	Model       string
	Temperature *float64
	Timeout     time.Duration
}

func NewOpenAIClient(cfg OpenAIConfig) *OpenAIClient {
	return &OpenAIClient{
		baseURL:     strings.TrimRight(cfg.BaseURL, "/"),
		apiKey:      cfg.APIKey,
		model:       cfg.Model,
		temperature: cfg.Temperature,
		http:        &http.Client{Timeout: cfg.Timeout},
	}
}

type chatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Tools       []Tool    `json:"tools,omitempty"`
	ToolChoice  string    `json:"tool_choice,omitempty"`
	Temperature *float64  `json:"temperature,omitempty"`
}

type chatResponse struct {
	Choices []struct {
		Message Message `json:"message"`
	} `json:"choices"`
}

// StatusError — ответ провайдера с кодом не 2xx.
type StatusError struct {
	Status int
	Body   string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("llm: status %d: %s", e.Status, e.Body)
}

func (e *StatusError) retryable() bool {
	return e.Status == http.StatusTooManyRequests || e.Status >= 500
}

func (c *OpenAIClient) Chat(ctx context.Context, messages []Message, tools []Tool) (Message, error) {
	req := chatRequest{Model: c.model, Messages: messages, Tools: tools, Temperature: c.temperature}
	if len(tools) > 0 {
		req.ToolChoice = "auto"
	}
	body, err := json.Marshal(req)
	if err != nil {
		return Message{}, fmt.Errorf("llm: marshal request: %w", err)
	}

	var lastErr error
	for attempt := range maxAttempts {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return Message{}, ctx.Err()
			case <-time.After(time.Duration(attempt) * time.Second):
			}
		}
		msg, err := c.do(ctx, body)
		if err == nil {
			return msg, nil
		}
		lastErr = err
		if !retryable(err) || ctx.Err() != nil {
			break
		}
	}
	return Message{}, lastErr
}

// transportError — сетевая ошибка запроса; её имеет смысл повторить.
type transportError struct{ err error }

func (e transportError) Error() string { return "llm: request: " + e.err.Error() }
func (e transportError) Unwrap() error { return e.err }

// retryable: повторяем только сетевые сбои и ответы 429/5xx; ошибки разбора ответа повтор не исправит.
func retryable(err error) bool {
	if se, ok := errors.AsType[*StatusError](err); ok {
		return se.retryable()
	}
	_, ok := errors.AsType[transportError](err)
	return ok
}

func (c *OpenAIClient) do(ctx context.Context, body []byte) (Message, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Message{}, fmt.Errorf("llm: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return Message{}, transportError{err}
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return Message{}, fmt.Errorf("llm: read response: %w", err)
	}
	if resp.StatusCode/100 != 2 {
		return Message{}, &StatusError{Status: resp.StatusCode, Body: truncate(string(data), 500)}
	}

	var out chatResponse
	if err := json.Unmarshal(data, &out); err != nil {
		return Message{}, fmt.Errorf("llm: decode response: %w", err)
	}
	if len(out.Choices) == 0 {
		// Некоторые провайдеры возвращают ошибку с кодом 200 в теле {"error": ...}.
		return Message{}, fmt.Errorf("llm: empty choices: %s", truncate(string(data), 500))
	}
	return out.Choices[0].Message, nil
}

// truncate обрезает s до n байт, не разрывая многобайтовый символ UTF-8.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n] + "…"
}
