package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"finance-ai-bot/internal/agent/llm"
	"finance-ai-bot/internal/agent/prompts"
	"finance-ai-bot/internal/agent/tools"
	"finance-ai-bot/internal/domain"
	"finance-ai-bot/internal/testutil/memstore"
	"finance-ai-bot/internal/usecase/expense"
	"finance-ai-bot/internal/usecase/user"
)

const (
	alice int64 = 111
	bob   int64 = 222
)

// scriptedLLM отвечает заранее заданными шагами; каждый шаг видит историю сообщений.
type scriptedLLM struct {
	steps []func(msgs []llm.Message) llm.Message
	calls int
	seen  [][]llm.Message
}

func (s *scriptedLLM) Chat(_ context.Context, msgs []llm.Message, _ []llm.Tool) (llm.Message, error) {
	if s.calls >= len(s.steps) {
		return llm.Message{}, fmt.Errorf("unexpected llm call #%d", s.calls+1)
	}
	step := s.steps[s.calls]
	s.calls++
	s.seen = append(s.seen, msgs)
	return step(msgs), nil
}

func toolCall(name string, args any) func([]llm.Message) llm.Message {
	return func([]llm.Message) llm.Message {
		data, _ := json.Marshal(args)
		return llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{
			ID: "call_" + name, Type: "function",
			Function: llm.FunctionCall{Name: name, Arguments: string(data)},
		}}}
	}
}

func say(text string) func([]llm.Message) llm.Message {
	return func([]llm.Message) llm.Message { return llm.Message{Role: llm.RoleAssistant, Content: text} }
}

type fixture struct {
	agent    *Agent
	expenses *expense.Service
	llm      *scriptedLLM
}

func newFixture(t *testing.T, steps ...func([]llm.Message) llm.Message) *fixture {
	t.Helper()
	store := memstore.New()
	users := user.NewService(store.Users())
	for _, id := range []int64{alice, bob} {
		if _, err := users.Upsert(context.Background(), domain.User{TelegramID: id, FirstName: "u"}); err != nil {
			t.Fatal(err)
		}
	}
	expenses := expense.NewService(store.Expenses(), time.UTC)
	client := &scriptedLLM{steps: steps}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	ai, err := New(client, expenses, Config{Location: time.UTC, MaxSteps: 4, Prompts: loadPrompts(t)}, log)
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{
		agent:    ai,
		expenses: expenses,
		llm:      client,
	}
}

// loadPrompts читает промпт из корня репозитория — тот же файл, что и бот.
func loadPrompts(t *testing.T) *prompts.Prompts {
	t.Helper()
	p, err := prompts.Load(filepath.Join("..", "..", "prompts", "system.md"))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func lastToolResult(msgs []llm.Message) map[string]any {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == llm.RoleTool {
			var out map[string]any
			_ = json.Unmarshal([]byte(msgs[i].Content), &out)
			return out
		}
	}
	return nil
}

func TestAddExpense(t *testing.T) {
	f := newFixture(t,
		toolCall(tools.AddExpense, map[string]any{"amount": 350, "category": "food", "comment": "кофе"}),
		say("Записал: кофе — 350 ₽ (еда), сегодня"),
	)
	reply, err := f.agent.Reply(context.Background(), alice, nil, "кофе 350")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply, "Записал") {
		t.Errorf("reply = %q", reply)
	}

	res, err := f.expenses.List(context.Background(), expense.UserScope(alice), domain.ExpenseFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 1 {
		t.Fatalf("got %d expenses, want 1", len(res.Items))
	}
	e := res.Items[0]
	if e.Amount != 35000 || e.Category != domain.CategoryFood || e.Comment != "кофе" || !e.SpentAt.Equal(f.expenses.Today()) {
		t.Errorf("unexpected expense: %+v", e)
	}
}

// Модель не может изменить чужую трату, даже если узнала её id.
func TestCannotTouchForeignExpense(t *testing.T) {
	f := newFixture(t)
	foreign, err := f.expenses.Create(context.Background(), expense.UserScope(bob), expense.CreateParams{
		Amount: 120000, Category: domain.CategoryTransport, Comment: "такси",
	})
	if err != nil {
		t.Fatal(err)
	}

	var results []map[string]any
	f.llm.steps = []func([]llm.Message) llm.Message{
		toolCall(tools.UpdateExpense, map[string]any{"id": foreign.ID.String(), "amount": 1}),
		func(msgs []llm.Message) llm.Message {
			results = append(results, lastToolResult(msgs))
			return toolCall(tools.DeleteExpense, map[string]any{"id": foreign.ID.String()})(msgs)
		},
		func(msgs []llm.Message) llm.Message {
			results = append(results, lastToolResult(msgs))
			return say("Не нашёл такую трату")(msgs)
		},
	}
	if _, err := f.agent.Reply(context.Background(), alice, nil, "удали такси"); err != nil {
		t.Fatal(err)
	}

	for i, r := range results {
		if r["error"] != "трата не найдена" {
			t.Errorf("tool result #%d = %v, want not found", i, r)
		}
	}
	got, err := f.expenses.Get(context.Background(), expense.UserScope(bob), foreign.ID)
	if err != nil || got.Amount != 120000 {
		t.Errorf("foreign expense changed: %+v, %v", got, err)
	}
}

func TestUpdateLastExpense(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	scope := expense.UserScope(alice)
	for _, c := range []string{"такси", "кофе"} {
		if _, err := f.expenses.Create(ctx, scope, expense.CreateParams{Amount: 35000, Category: domain.CategoryFood, Comment: c}); err != nil {
			t.Fatal(err)
		}
	}

	var lastID string
	f.llm.steps = []func([]llm.Message) llm.Message{
		toolCall(tools.ListExpenses, map[string]any{"sort": "created_at", "limit": 1}),
		func(msgs []llm.Message) llm.Message {
			items := lastToolResult(msgs)["items"].([]any)
			if len(items) != 1 {
				t.Fatalf("list returned %d items", len(items))
			}
			item := items[0].(map[string]any)
			if item["comment"] != "кофе" {
				t.Errorf("last added = %v, want кофе", item["comment"])
			}
			lastID = item["id"].(string)
			return toolCall(tools.UpdateExpense, map[string]any{"id": lastID, "amount": 450})(msgs)
		},
		say("Исправил: кофе — 450 ₽"),
	}
	if _, err := f.agent.Reply(ctx, alice, nil, "последнее было не 350, а 450"); err != nil {
		t.Fatal(err)
	}

	res, _ := f.expenses.List(ctx, scope, domain.ExpenseFilter{Query: "кофе"})
	if res.Items[0].Amount != 45000 {
		t.Errorf("amount = %v, want 450 ₽", res.Items[0].Amount)
	}
}

func TestToolErrorsAreReportedToModel(t *testing.T) {
	var result map[string]any
	f := newFixture(t,
		toolCall(tools.AddExpense, map[string]any{"amount": -5, "category": "food"}),
		func(msgs []llm.Message) llm.Message {
			result = lastToolResult(msgs)
			return say("Сумма должна быть больше нуля")(msgs)
		},
	)
	if _, err := f.agent.Reply(context.Background(), alice, nil, "кофе -5"); err != nil {
		t.Fatal(err)
	}
	if msg, _ := result["error"].(string); !strings.Contains(msg, "amount") {
		t.Errorf("tool result = %v, want amount validation error", result)
	}
}

func TestStepLimit(t *testing.T) {
	loop := toolCall(tools.ListExpenses, map[string]any{})
	f := newFixture(t, loop, loop, loop, loop, loop)
	reply, err := f.agent.Reply(context.Background(), alice, nil, "покажи траты")
	if err != nil {
		t.Fatal(err)
	}
	if reply != fallbackReply {
		t.Errorf("reply = %q, want fallback", reply)
	}
	if f.llm.calls != 4 {
		t.Errorf("llm calls = %d, want MaxSteps=4", f.llm.calls)
	}
}

func TestHistoryIsPassed(t *testing.T) {
	var seen []llm.Message
	f := newFixture(t, func(msgs []llm.Message) llm.Message {
		seen = msgs
		return say("ok")(msgs)
	})
	history := []domain.ChatMessage{
		{Role: domain.ChatRoleUser, Content: "удали такси"},
		{Role: domain.ChatRoleAssistant, Content: "Какое такси удалить?"},
	}
	if _, err := f.agent.Reply(context.Background(), alice, history, "первое"); err != nil {
		t.Fatal(err)
	}
	roles := make([]string, len(seen))
	for i, m := range seen {
		roles[i] = m.Role
	}
	want := "system,user,assistant,user"
	if got := strings.Join(roles, ","); got != want {
		t.Errorf("roles = %s, want %s", got, want)
	}
}

// Промпт ссылается на инструменты по имени — имена должны совпадать с объявленными.
func TestSystemPromptMentionsEveryTool(t *testing.T) {
	f := newFixture(t, say("ок"))
	if _, err := f.agent.Reply(context.Background(), alice, nil, "привет"); err != nil {
		t.Fatal(err)
	}
	system := f.llm.seen[0][0].Content
	for _, d := range f.agent.tools.Definitions() {
		if !strings.Contains(system, d.Function.Name) {
			t.Errorf("system prompt does not mention tool %q", d.Function.Name)
		}
	}
}

// Модели часто присылают все поля схемы с пустыми значениями — они не должны затирать данные.
func TestUpdateIgnoresEmptyFields(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	e, err := f.expenses.Create(ctx, expense.UserScope(alice), expense.CreateParams{
		Amount: 35000, Category: domain.CategoryFood, Comment: "кофе",
	})
	if err != nil {
		t.Fatal(err)
	}
	f.llm.steps = []func([]llm.Message) llm.Message{
		toolCall(tools.UpdateExpense, map[string]any{"id": e.ID.String(), "amount": "450", "category": "", "spent_at": "", "comment": ""}),
		say("Исправил"),
	}
	if _, err := f.agent.Reply(ctx, alice, nil, "последнее было не 350, а 450"); err != nil {
		t.Fatal(err)
	}
	got, _ := f.expenses.Get(ctx, expense.UserScope(alice), e.ID)
	if got.Amount != 45000 || got.Comment != "кофе" || got.Category != domain.CategoryFood || !got.SpentAt.Equal(e.SpentAt) {
		t.Errorf("after update: %+v", got)
	}
}

// Если LLM упала после записи, пользователь узнаёт о сохранённой трате, а не получает ошибку.
func TestReportsChangesWhenLLMFails(t *testing.T) {
	f := newFixture(t,
		toolCall(tools.AddExpense, map[string]any{"amount": "1 200,50", "category": "transport", "comment": "такси"}),
		// следующего шага нет — scriptedLLM вернёт ошибку
	)
	reply, err := f.agent.Reply(context.Background(), alice, nil, "такси 1200,50")
	if err != nil {
		t.Fatalf("err = %v, want summary reply", err)
	}
	if !strings.Contains(reply, "записал: такси — 1 200,50 ₽ (транспорт)") {
		t.Errorf("reply = %q", reply)
	}
}

func TestReportsChangesOnStepLimit(t *testing.T) {
	add := toolCall(tools.AddExpense, map[string]any{"amount": 350, "category": "food", "comment": "кофе"})
	list := toolCall(tools.ListExpenses, map[string]any{})
	f := newFixture(t, add, list, list, list)
	reply, err := f.agent.Reply(context.Background(), alice, nil, "кофе 350")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply, "записал: кофе") {
		t.Errorf("reply = %q", reply)
	}
}

// Модель скопировала «Записал…» из истории, не вызвав инструмент: агент просит её
// сохранить трату по-настоящему, и пользователь получает правдивый ответ.
func TestUnconfirmedChangeIsRetried(t *testing.T) {
	var nudged bool
	f := newFixture(t,
		say("Записал: такси — 1 200 ₽ (транспорт), вчера"),
		func(msgs []llm.Message) llm.Message {
			last := msgs[len(msgs)-1]
			nudged = last.Role == llm.RoleSystem && strings.Contains(last.Content, "ничего не сохранено")
			return toolCall(tools.AddExpense, map[string]any{"amount": 1200, "category": "transport", "comment": "такси"})(msgs)
		},
		say("Записал: такси — 1 200 ₽ (транспорт), сегодня"),
	)
	reply, err := f.agent.Reply(context.Background(), alice, nil, "такси 1200")
	if err != nil {
		t.Fatal(err)
	}
	if !nudged {
		t.Error("model was not told that nothing was saved")
	}
	if !strings.HasPrefix(reply, "Записал: такси") {
		t.Errorf("reply = %q", reply)
	}
	res, err := f.expenses.List(context.Background(), expense.UserScope(alice), domain.ExpenseFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 1 {
		t.Fatalf("got %d expenses, want 1", len(res.Items))
	}
}

// Если модель и после подсказки только утверждает, что сохранила, — не обманываем пользователя.
func TestUnconfirmedChangeIsNotReported(t *testing.T) {
	f := newFixture(t,
		say("Записал: такси — 1 200 ₽ (транспорт), вчера"),
		say("Записал: такси — 1 200 ₽ (транспорт), вчера"),
	)
	reply, err := f.agent.Reply(context.Background(), alice, nil, "такси 1200")
	if err != nil {
		t.Fatal(err)
	}
	if reply != unconfirmedReply {
		t.Errorf("reply = %q, want %q", reply, unconfirmedReply)
	}
}

// Ответ без заявления об изменении не перепроверяется: лишнего запроса к LLM нет.
func TestPlainReplyIsNotRetried(t *testing.T) {
	f := newFixture(t, say("Сколько стоило такси?"))
	reply, err := f.agent.Reply(context.Background(), alice, nil, "такси")
	if err != nil {
		t.Fatal(err)
	}
	if reply != "Сколько стоило такси?" || f.llm.calls != 1 {
		t.Errorf("reply = %q, calls = %d", reply, f.llm.calls)
	}
}

func TestClaimsChange(t *testing.T) {
	cases := map[string]bool{
		"Записал: кофе — 350 ₽ (еда), сегодня": true,
		"записала кофе":                        true,
		"Изменил: кофе — 450 ₽":                true,
		"Исправил сумму на 450 ₽":              true,
		"Удалил: такси — 1 200 ₽":              true,
		"Готово, удалил такси":                 true,
		"Не нашёл такси, ничего не удалил":     false,
		"В этом месяце на еду 350 ₽":           false,
		"Сколько стоило такси?":                false,
	}
	for text, want := range cases {
		if got := claimsChange(text); got != want {
			t.Errorf("claimsChange(%q) = %v, want %v", text, got, want)
		}
	}
}

func TestLLMErrorWithoutChanges(t *testing.T) {
	f := newFixture(t)
	if _, err := f.agent.Reply(context.Background(), alice, nil, "кофе 350"); err == nil {
		t.Fatal("want error when nothing was done")
	}
}

func TestNormalizeToolCall(t *testing.T) {
	var seen []llm.Message
	f := newFixture(t,
		func([]llm.Message) llm.Message {
			return llm.Message{ToolCalls: []llm.ToolCall{{Function: llm.FunctionCall{Name: tools.ListExpenses, Arguments: `{"limit": 5.0}`}}}}
		},
		func(msgs []llm.Message) llm.Message {
			seen = msgs
			return say("ok")(msgs)
		},
	)
	if _, err := f.agent.Reply(context.Background(), alice, nil, "покажи траты"); err != nil {
		t.Fatal(err)
	}
	call := seen[len(seen)-2].ToolCalls[0]
	result := seen[len(seen)-1]
	if call.ID == "" || call.Type != "function" || result.ToolCallID != call.ID {
		t.Errorf("call = %+v, tool result id = %q", call, result.ToolCallID)
	}
	if strings.Contains(result.Content, "error") {
		t.Errorf("limit 5.0 rejected: %s", result.Content)
	}
}

// Неполная конфигурация отклоняется при создании, а не паникой на первом сообщении.
func TestNewRejectsIncompleteConfig(t *testing.T) {
	p := loadPrompts(t)
	tests := map[string]Config{
		"no prompts":  {Location: time.UTC, MaxSteps: 4},
		"no location": {Prompts: p, MaxSteps: 4},
		"zero steps":  {Prompts: p, Location: time.UTC},
	}
	for name, cfg := range tests {
		t.Run(name, func(t *testing.T) {
			ai, err := New(&scriptedLLM{}, nil, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
			if err == nil || ai != nil {
				t.Errorf("New = %v, %v; want error", ai, err)
			}
		})
	}
}
