// Package tools — инструменты ИИ-агента. Каждый инструмент в своём файле:
// JSON-схема для модели, разбор аргументов и вызов сценария учёта трат.
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"finance-ai-bot/internal/agent/llm"
	"finance-ai-bot/internal/domain"
	"finance-ai-bot/internal/usecase/expense"
)

// ExpenseUseCase — сценарии учёта трат, которые вызывают инструменты.
type ExpenseUseCase interface {
	Create(ctx context.Context, scope expense.Scope, in expense.CreateParams) (domain.Expense, error)
	List(ctx context.Context, scope expense.Scope, f domain.ExpenseFilter) (expense.ListResult, error)
	Summary(ctx context.Context, scope expense.Scope, f domain.ExpenseFilter) (domain.ExpenseSummary, error)
	Update(ctx context.Context, scope expense.Scope, id uuid.UUID, p domain.ExpensePatch) (domain.Expense, error)
	Delete(ctx context.Context, scope expense.Scope, id uuid.UUID) (domain.Expense, error)
}

// Journal копит изменения трат за ход агента: пользователь узнает о них, даже если модель потом не ответит.
// Изменяющие инструменты обязаны вызывать Record после успешной операции.
type Journal interface {
	Record(action string, e domain.Expense)
}

// Turn — контекст одного хода агента: чьи траты доступны и куда записывать изменения.
type Turn struct {
	Scope   expense.Scope
	Journal Journal
}

type tool struct {
	def llm.Tool
	run func(ctx context.Context, turn Turn, raw []byte) (any, error)
}

// Set — набор инструментов агента.
type Set struct {
	expenses ExpenseUseCase
	loc      *time.Location
	log      *slog.Logger
	byName   map[string]tool
	defs     []llm.Tool
}

func NewSet(expenses ExpenseUseCase, loc *time.Location, log *slog.Logger) *Set {
	s := &Set{expenses: expenses, loc: loc, log: log, byName: map[string]tool{}}
	for _, t := range []tool{s.addTool(), s.listTool(), s.sumTool(), s.updateTool(), s.deleteTool()} {
		s.byName[t.def.Function.Name] = t
		s.defs = append(s.defs, t.def)
	}
	return s
}

// Definitions — описания инструментов для запроса к модели.
func (s *Set) Definitions() []llm.Tool { return s.defs }

// Run выполняет вызов инструмента и всегда возвращает JSON для модели:
// результат или {"error": "..."}, чтобы модель могла исправиться или объяснить пользователю.
func (s *Set) Run(ctx context.Context, turn Turn, call llm.ToolCall) string {
	result, err := s.dispatch(ctx, turn, call.Function.Name, []byte(call.Function.Arguments))
	if err != nil {
		var (
			te  toolError
			ve  *domain.ValidationError
			msg string
		)
		switch {
		case errors.As(err, &te):
			msg = te.msg
		case errors.As(err, &ve):
			msg = ve.Error()
		case errors.Is(err, domain.ErrNotFound):
			msg = "трата не найдена"
		default:
			s.log.ErrorContext(ctx, "tool failed", "tool", call.Function.Name, "error", err)
			msg = "внутренняя ошибка, операция не выполнена"
		}
		result = map[string]string{"error": msg}
	}
	data, err := json.Marshal(result)
	if err != nil {
		return `{"error":"internal error"}`
	}
	return string(data)
}

func (s *Set) dispatch(ctx context.Context, turn Turn, name string, raw []byte) (any, error) {
	t, ok := s.byName[name]
	if !ok {
		return nil, toolError{fmt.Sprintf("неизвестный инструмент %q", name)}
	}
	return t.run(ctx, turn, raw)
}

// handle разбирает аргументы вызова в A и передаёт их обработчику инструмента.
func handle[A any](fn func(ctx context.Context, turn Turn, args A) (any, error)) func(context.Context, Turn, []byte) (any, error) {
	return func(ctx context.Context, turn Turn, raw []byte) (any, error) {
		var args A
		if err := decodeArgs(raw, &args); err != nil {
			return nil, err
		}
		return fn(ctx, turn, args)
	}
}

func definition(name, desc, params string) llm.Tool {
	return llm.Tool{Type: "function", Function: llm.FunctionDef{
		Name: name, Description: desc, Parameters: json.RawMessage(params),
	}}
}

// categoryEnum — коды категорий для JSON Schema: "food", "transport", ...
var categoryEnum = func() string {
	q := make([]string, len(domain.Categories))
	for i, c := range domain.Categories {
		q[i] = `"` + string(c) + `"`
	}
	return strings.Join(q, ", ")
}()

// toolError — ошибка, которую модель должна увидеть и учесть в ответе.
type toolError struct{ msg string }

func (e toolError) Error() string { return e.msg }
