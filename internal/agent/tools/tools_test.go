package tools

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"
	"time"

	"finance-ai-bot/internal/adapter/storage/memory"
	"finance-ai-bot/internal/agent/llm"
	"finance-ai-bot/internal/domain"
	"finance-ai-bot/internal/usecase/expense"
)

const alice int64 = 111

type recorder struct{ actions []string }

func (r *recorder) Record(action string, _ domain.Expense) { r.actions = append(r.actions, action) }

func newSet(t *testing.T) (*Set, *recorder, Turn) {
	t.Helper()
	store := memory.New()
	if _, err := store.Users().Upsert(context.Background(), domain.User{TelegramID: alice, FirstName: "u"}); err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	rec := &recorder{}
	set := NewSet(expense.NewService(store.Expenses(), time.UTC), time.UTC, log)
	return set, rec, Turn{Scope: expense.UserScope(alice), Journal: rec}
}

func run(t *testing.T, set *Set, turn Turn, name, args string) map[string]any {
	t.Helper()
	out := set.Run(context.Background(), turn, llm.ToolCall{Function: llm.FunctionCall{Name: name, Arguments: args}})
	var res map[string]any
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("result is not JSON: %s", out)
	}
	return res
}

func TestDefinitionsAreValidJSONSchema(t *testing.T) {
	set, _, _ := newSet(t)
	names := map[string]bool{}
	for _, d := range set.Definitions() {
		var schema map[string]any
		if err := json.Unmarshal(d.Function.Parameters, &schema); err != nil {
			t.Errorf("%s: invalid schema: %v", d.Function.Name, err)
		}
		names[d.Function.Name] = true
	}
	for _, n := range []string{AddExpense, ListExpenses, SumExpenses, UpdateExpense, DeleteExpense} {
		if !names[n] {
			t.Errorf("tool %s is not defined", n)
		}
	}
}

func TestMutatingToolsRecordJournal(t *testing.T) {
	set, rec, turn := newSet(t)
	added := run(t, set, turn, AddExpense, `{"amount": "1 200,50", "category": "food", "comment": "обед"}`)
	view, ok := added["added"].(map[string]any)
	if !ok || view["amount"] != 1200.5 {
		t.Fatalf("add result = %v", added)
	}
	id := view["id"].(string)
	run(t, set, turn, UpdateExpense, `{"id": "`+id+`", "amount": 1300}`)
	run(t, set, turn, DeleteExpense, `{"id": "`+id+`"}`)

	want := []string{"записал", "изменил", "удалил"}
	if len(rec.actions) != len(want) {
		t.Fatalf("journal = %v, want %v", rec.actions, want)
	}
	for i := range want {
		if rec.actions[i] != want[i] {
			t.Errorf("journal[%d] = %q, want %q", i, rec.actions[i], want[i])
		}
	}
}

func TestToolErrors(t *testing.T) {
	set, rec, turn := newSet(t)
	tests := []struct {
		name, tool, args, want string
	}{
		{"unknown tool", "drop_table", `{}`, `неизвестный инструмент "drop_table"`},
		{"bad json", AddExpense, `{`, ""},
		{"bad amount", AddExpense, `{"amount": "много", "category": "food"}`, ""},
		{"bad date", AddExpense, `{"amount": 1, "category": "food", "spent_at": "вчера"}`, "spent_at: ожидается дата YYYY-MM-DD"},
		{"bad id", DeleteExpense, `{"id": "42"}`, "некорректный id — возьми id из list_expenses"},
		{"empty patch", UpdateExpense, `{"id": "00000000-0000-0000-0000-000000000001", "amount": 0, "comment": " "}`, "не передано ни одного изменяемого поля"},
		{"not found", DeleteExpense, `{"id": "00000000-0000-0000-0000-000000000001"}`, "трата не найдена"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := run(t, set, turn, tt.tool, tt.args)
			msg, ok := res["error"].(string)
			if !ok {
				t.Fatalf("result = %v, want error", res)
			}
			if tt.want != "" && msg != tt.want {
				t.Errorf("error = %q, want %q", msg, tt.want)
			}
		})
	}
	if len(rec.actions) != 0 {
		t.Errorf("failed calls recorded changes: %v", rec.actions)
	}
}

func TestListLimitIsClamped(t *testing.T) {
	set, _, turn := newSet(t)
	for range maxListLimit + 5 {
		run(t, set, turn, AddExpense, `{"amount": 1, "category": "other"}`)
	}
	res := run(t, set, turn, ListExpenses, `{"limit": 1000}`)
	if items := res["items"].([]any); len(items) != maxListLimit {
		t.Errorf("items = %d, want %d", len(items), maxListLimit)
	}
	if res["count"] != float64(maxListLimit+5) {
		t.Errorf("count = %v", res["count"])
	}
}
