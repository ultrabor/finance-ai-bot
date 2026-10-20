package llm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"
)

func newTestClient(url string) *OpenAIClient {
	return NewOpenAIClient(OpenAIConfig{BaseURL: url + "/", APIKey: "sk-test", Model: "m", Timeout: 5 * time.Second})
}

func TestChatWireFormat(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" || r.Header.Get("Authorization") != "Bearer sk-test" {
			t.Errorf("path=%s auth=%q", r.URL.Path, r.Header.Get("Authorization"))
		}
		data, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(data, &body)
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":null,"tool_calls":[{"id":"c1","type":"function","function":{"name":"f","arguments":"{}"}}]}}]}`)
	}))
	defer srv.Close()

	msgs := []Message{
		{Role: RoleUser, Content: "кофе 350"},
		{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "c0", Type: "function", Function: FunctionCall{Name: "f", Arguments: "{}"}}}},
		{Role: RoleTool, ToolCallID: "c0", Content: "{}"},
	}
	got, err := newTestClient(srv.URL).Chat(context.Background(), msgs, []Tool{{Type: "function"}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Content != "" || len(got.ToolCalls) != 1 || got.ToolCalls[0].ID != "c1" {
		t.Errorf("response = %+v", got)
	}

	sent := body["messages"].([]any)
	assistant := sent[1].(map[string]any)
	if c, ok := assistant["content"]; !ok || c != nil {
		t.Errorf("assistant with tool_calls must send content: null, got %v (present=%v)", c, ok)
	}
	if sent[0].(map[string]any)["content"] != "кофе 350" {
		t.Errorf("user message = %v", sent[0])
	}
	if body["tool_choice"] != "auto" || body["model"] != "m" {
		t.Errorf("request = %v", body)
	}
	if _, ok := body["temperature"]; ok {
		t.Error("temperature must be omitted when not configured")
	}
}

func TestChatRetries(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		if len(data) == 0 {
			t.Error("empty body on retry")
		}
		if calls.Add(1) == 1 {
			http.Error(w, "overloaded", http.StatusServiceUnavailable)
			return
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`)
	}))
	defer srv.Close()

	got, err := newTestClient(srv.URL).Chat(context.Background(), []Message{{Role: RoleUser, Content: "x"}}, nil)
	if err != nil || got.Content != "ok" || calls.Load() != 2 {
		t.Fatalf("got %+v, err %v, calls %d", got, err, calls.Load())
	}
}

func TestRetryDelay(t *testing.T) {
	if d := retryDelay(&StatusError{Status: 429, RetryAfter: 3 * time.Second}, 1); d != 3*time.Second {
		t.Errorf("Retry-After: delay = %v", d)
	}
	if d := retryDelay(&StatusError{Status: 429, RetryAfter: time.Hour}, 1); d != maxRetryAfter {
		t.Errorf("capped delay = %v", d)
	}
	if d := retryDelay(&StatusError{Status: 503}, 2); d != 2*time.Second {
		t.Errorf("backoff delay = %v", d)
	}
}

func TestStatusErrorRetryAfter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "7")
		http.Error(w, "slow down", http.StatusTooManyRequests)
	}))
	defer srv.Close()

	_, err := newTestClient(srv.URL).do(context.Background(), []byte("{}"))
	se, ok := errors.AsType[*StatusError](err)
	if !ok || se.RetryAfter != 7*time.Second {
		t.Fatalf("err = %v", err)
	}
}

func TestChatNoRetryOnClientError(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, `{"error":"bad model"}`, http.StatusBadRequest)
	}))
	defer srv.Close()

	_, err := newTestClient(srv.URL).Chat(context.Background(), []Message{{Role: RoleUser, Content: "x"}}, nil)
	se, ok := errors.AsType[*StatusError](err)
	if !ok || se.Status != http.StatusBadRequest || calls.Load() != 1 {
		t.Fatalf("err = %v, calls = %d", err, calls.Load())
	}
}

func TestChatEmptyChoicesIncludesBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"error":{"message":"quota exceeded"}}`)
	}))
	defer srv.Close()

	_, err := newTestClient(srv.URL).Chat(context.Background(), []Message{{Role: RoleUser, Content: "x"}}, nil)
	if err == nil || !strings.Contains(err.Error(), "quota exceeded") {
		t.Fatalf("err = %v", err)
	}
}

func TestTruncateKeepsUTF8(t *testing.T) {
	s := strings.Repeat("я", 10) // 20 байт
	got := truncate(s, 7)
	if !utf8.ValidString(got) || got != "яяя…" {
		t.Errorf("truncate = %q", got)
	}
}
