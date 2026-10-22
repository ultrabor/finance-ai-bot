package tools

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"finance-ai-bot/internal/agent/llm"
	"finance-ai-bot/internal/domain"
	"finance-ai-bot/internal/testutil/memstore"
	"finance-ai-bot/internal/usecase/expense"
)

const alice int64 = 111

type recorder struct{ actions []string }

func (r *recorder) Record(action string, _ domain.Expense) { r.actions = append(r.actions, action) }

func newSet(t *testing.T) (*Set, *recorder, Turn) {
	t.Helper()
	store := memstore.New()
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

func TestSumByCategory(t *testing.T) {
	set, _, turn := newSet(t)
	run(t, set, turn, AddExpense, `{"amount": 350, "category": "food", "spent_at": "2026-01-10"}`)
	run(t, set, turn, AddExpense, `{"amount": 150, "category": "food", "spent_at": "2026-01-11"}`)
	run(t, set, turn, AddExpense, `{"amount": 1200, "category": "transport", "spent_at": "2026-01-11"}`)
	run(t, set, turn, AddExpense, `{"amount": 99, "category": "food", "spent_at": "2026-02-01"}`)

	res := run(t, set, turn, SumExpenses, `{"from": "2026-01-01", "to": "2026-01-31"}`)
	if res["count"] != 3.0 || res["total"] != 1700.0 {
		t.Fatalf("january sum = %v", res)
	}
	byCat := res["by_category"].([]any)
	if len(byCat) != 2 {
		t.Fatalf("by_category = %v", byCat)
	}
	// Категории подписаны по-русски, как их назовёт модель.
	labels := map[string]float64{}
	for _, c := range byCat {
		m := c.(map[string]any)
		labels[m["category"].(string)] = m["amount"].(float64)
	}
	if labels["еда"] != 500 || labels["транспорт"] != 1200 {
		t.Errorf("by_category = %v", labels)
	}

	res = run(t, set, turn, SumExpenses, `{"category": "food"}`)
	if res["count"] != 3.0 || res["total"] != 599.0 {
		t.Errorf("food sum = %v", res)
	}
}

func TestListFilters(t *testing.T) {
	set, _, turn := newSet(t)
	run(t, set, turn, AddExpense, `{"amount": 350, "category": "food", "spent_at": "2026-01-10", "comment": "кофе"}`)
	run(t, set, turn, AddExpense, `{"amount": 900, "category": "transport", "spent_at": "2026-01-12", "comment": "Такси домой"}`)
	run(t, set, turn, AddExpense, `{"amount": 700, "category": "transport", "spent_at": "2026-02-03", "comment": "такси"}`)

	tests := []struct {
		name, args string
		count      float64
	}{
		{"query is case-insensitive", `{"query": "такси"}`, 2},
		{"period", `{"from": "2026-01-01", "to": "2026-01-31"}`, 2},
		{"category", `{"category": "food"}`, 1},
		{"period and query", `{"from": "2026-02-01", "query": "такси"}`, 1},
		{"limit as string", `{"limit": "1"}`, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := run(t, set, turn, ListExpenses, tt.args)
			if res["count"] != tt.count {
				t.Errorf("count = %v, want %v (%v)", res["count"], tt.count, res)
			}
		})
	}

	res := run(t, set, turn, ListExpenses, `{"sort": "created_at", "limit": 1}`)
	items := res["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["comment"] != "такси" {
		t.Errorf("last added = %v", items)
	}
}

func TestUpdateAllFields(t *testing.T) {
	set, _, turn := newSet(t)
	added := run(t, set, turn, AddExpense, `{"amount": 350, "category": "food", "comment": "кофе"}`)
	id := added["added"].(map[string]any)["id"].(string)

	res := run(t, set, turn, UpdateExpense,
		`{"id": "`+id+`", "amount": "450", "category": "entertainment", "spent_at": "2026-01-05", "comment": " кино "}`)
	got, ok := res["updated"].(map[string]any)
	if !ok {
		t.Fatalf("update result = %v", res)
	}
	want := map[string]any{"amount": 450.0, "category": "развлечения", "spent_at": "2026-01-05", "comment": "кино"}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %v, want %v", k, got[k], v)
		}
	}
}

func TestArgumentErrors(t *testing.T) {
	set, _, turn := newSet(t)
	id := "00000000-0000-0000-0000-000000000001"
	tests := []struct {
		name, tool, args, want string
	}{
		{"bad from", ListExpenses, `{"from": "январь"}`, "from: ожидается дата YYYY-MM-DD"},
		{"bad to", SumExpenses, `{"to": "31.01.2026"}`, "to: ожидается дата YYYY-MM-DD"},
		{"amount not a number", AddExpense, `{"amount": true, "category": "food"}`, ""},
		{"too big amount", AddExpense, `{"amount": 1e12, "category": "food"}`, "некорректная сумма"},
		{"update bad amount", UpdateExpense, `{"id": "` + id + `", "amount": 1e12}`, "некорректная сумма"},
		{"update bad date", UpdateExpense, `{"id": "` + id + `", "spent_at": "вчера"}`, "spent_at: ожидается дата YYYY-MM-DD"},
		{"update bad id", UpdateExpense, `{"id": "last", "amount": 5}`, "некорректный id — возьми id из list_expenses"},
		{"bad category", SumExpenses, `{"category": "beer"}`, ""},
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
}

func TestEmptyArgumentsMeanEmptyObject(t *testing.T) {
	set, _, turn := newSet(t)
	if res := run(t, set, turn, SumExpenses, ``); res["count"] != 0.0 {
		t.Errorf("sum without args = %v", res)
	}
}

// failingExpenses — use case, у которого отказало хранилище.
type failingExpenses struct{ ExpenseUseCase }

func (failingExpenses) Summary(context.Context, expense.Scope, domain.ExpenseFilter) (domain.ExpenseSummary, error) {
	return domain.ExpenseSummary{}, errors.New("connection refused: 10.0.0.5:5432")
}

// Неожиданная ошибка логируется, а модель получает общее сообщение без деталей.
func TestInternalErrorIsHiddenFromModel(t *testing.T) {
	var logs strings.Builder
	log := slog.New(slog.NewTextHandler(&logs, nil))
	set := NewSet(failingExpenses{}, time.UTC, log)

	res := run(t, set, Turn{Scope: expense.UserScope(alice), Journal: &recorder{}}, SumExpenses, `{}`)
	if res["error"] != "внутренняя ошибка, операция не выполнена" {
		t.Errorf("result = %v", res)
	}
	if !strings.Contains(logs.String(), "connection refused") {
		t.Errorf("internal error was not logged: %q", logs.String())
	}
}
