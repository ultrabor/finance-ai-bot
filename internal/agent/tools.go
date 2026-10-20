package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"finance-ai-bot/internal/domain"
	"finance-ai-bot/internal/llm"
	"finance-ai-bot/internal/service"
)

const (
	toolAdd    = "add_expense"
	toolList   = "list_expenses"
	toolSum    = "sum_expenses"
	toolUpdate = "update_expense"
	toolDelete = "delete_expense"

	defaultToolListLimit = 10
	maxToolListLimit     = 50
)

func toolDefinitions() []llm.Tool {
	cats := quoteAll(categoryEnum())
	def := func(name, desc, params string) llm.Tool {
		return llm.Tool{Type: "function", Function: llm.FunctionDef{
			Name: name, Description: desc, Parameters: json.RawMessage(params),
		}}
	}
	return []llm.Tool{
		def(toolAdd, "Записать новую трату пользователя.", `{
			"type": "object",
			"properties": {
				"amount":   {"type": "number", "description": "Сумма в рублях, больше 0"},
				"category": {"type": "string", "enum": [`+cats+`]},
				"spent_at": {"type": "string", "description": "Дата траты YYYY-MM-DD; по умолчанию сегодня"},
				"comment":  {"type": "string", "description": "Короткое описание: «кофе», «такси до работы»"}
			},
			"required": ["amount", "category"]
		}`),
		def(toolList, "Найти траты пользователя. Возвращает до limit трат с id, а также количество и сумму всех найденных.", `{
			"type": "object",
			"properties": {
				"from":     {"type": "string", "description": "Дата с (включительно), YYYY-MM-DD"},
				"to":       {"type": "string", "description": "Дата по (включительно), YYYY-MM-DD"},
				"category": {"type": "string", "enum": [`+cats+`]},
				"query":    {"type": "string", "description": "Подстрока описания траты, например «такси»"},
				"sort":     {"type": "string", "enum": ["spent_at", "created_at"], "description": "spent_at — по дате траты (по умолчанию), created_at — по времени добавления; всегда от новых к старым"},
				"limit":    {"type": "integer", "description": "Сколько трат вернуть, 1–50, по умолчанию 10"}
			}
		}`),
		def(toolSum, "Посчитать сумму трат пользователя за период с разбивкой по категориям.", `{
			"type": "object",
			"properties": {
				"from":     {"type": "string", "description": "Дата с (включительно), YYYY-MM-DD"},
				"to":       {"type": "string", "description": "Дата по (включительно), YYYY-MM-DD"},
				"category": {"type": "string", "enum": [`+cats+`]}
			}
		}`),
		def(toolUpdate, "Изменить трату пользователя. Передавай только изменяемые поля; id бери из list_expenses.", `{
			"type": "object",
			"properties": {
				"id":       {"type": "string", "description": "id траты из list_expenses"},
				"amount":   {"type": "number", "description": "Новая сумма в рублях"},
				"category": {"type": "string", "enum": [`+cats+`]},
				"spent_at": {"type": "string", "description": "Новая дата YYYY-MM-DD"},
				"comment":  {"type": "string", "description": "Новое описание; не передавай, если описание не меняется"}
			},
			"required": ["id"]
		}`),
		def(toolDelete, "Удалить трату пользователя; id бери из list_expenses.", `{
			"type": "object",
			"properties": {
				"id": {"type": "string", "description": "id траты из list_expenses"}
			},
			"required": ["id"]
		}`),
	}
}

type expenseView struct {
	ID       string  `json:"id"`
	Amount   float64 `json:"amount"`
	Category string  `json:"category"`
	SpentAt  string  `json:"spent_at"`
	Comment  string  `json:"comment,omitempty"`
	AddedAt  string  `json:"added_at"`
}

type categoryTotalView struct {
	Category string  `json:"category"`
	Amount   float64 `json:"amount"`
	Count    int     `json:"count"`
}

type summaryView struct {
	Count      int                 `json:"count"`
	Total      float64             `json:"total"`
	ByCategory []categoryTotalView `json:"by_category,omitempty"`
}

type listView struct {
	Items []expenseView `json:"items"`
	Count int           `json:"count"`
	Total float64       `json:"total"`
}

// flexNumber принимает число или строку с числом: модели иногда присылают "350" или "1 200,50".
type flexNumber float64

func (n *flexNumber) UnmarshalJSON(b []byte) error {
	var f float64
	if err := json.Unmarshal(b, &f); err == nil {
		*n = flexNumber(f)
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return errors.New("ожидается число")
	}
	clean := strings.NewReplacer(" ", "", "\u00a0", "", ",", ".", "₽", "").Replace(s)
	if clean == "" {
		*n = 0 // пустая строка — «не передано»
		return nil
	}
	f, err := strconv.ParseFloat(clean, 64)
	if err != nil {
		return fmt.Errorf("ожидается число, получено %q", s)
	}
	*n = flexNumber(f)
	return nil
}

type addArgs struct {
	Amount   flexNumber `json:"amount"`
	Category string     `json:"category"`
	SpentAt  string     `json:"spent_at"`
	Comment  string     `json:"comment"`
}

type periodArgs struct {
	From     string `json:"from"`
	To       string `json:"to"`
	Category string `json:"category"`
}

type listArgs struct {
	periodArgs
	Query string     `json:"query"`
	Sort  string     `json:"sort"`
	Limit flexNumber `json:"limit"`
}

// updateArgs: пустые значения ("" и 0) означают «не менять» — модели часто присылают все поля схемы.
type updateArgs struct {
	ID       string     `json:"id"`
	Amount   flexNumber `json:"amount"`
	Category string     `json:"category"`
	SpentAt  string     `json:"spent_at"`
	Comment  string     `json:"comment"`
}

// mutationView — результат изменяющего инструмента.
type mutationView struct {
	Added   *expenseView `json:"added,omitempty"`
	Updated *expenseView `json:"updated,omitempty"`
	Deleted *expenseView `json:"deleted,omitempty"`
}

type deleteArgs struct {
	ID string `json:"id"`
}

// toolError — ошибка, которую модель должна увидеть и учесть в ответе.
type toolError struct{ msg string }

func (e toolError) Error() string { return e.msg }

// runTool выполняет вызов инструмента и всегда возвращает JSON для модели:
// результат или {"error": "..."}, чтобы модель могла исправиться или объяснить пользователю.
func (a *Agent) runTool(ctx context.Context, scope service.Scope, done *journal, call llm.ToolCall) string {
	result, err := a.dispatch(ctx, scope, done, call.Function.Name, []byte(call.Function.Arguments))
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
			a.log.ErrorContext(ctx, "tool failed", "tool", call.Function.Name, "error", err)
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

func (a *Agent) dispatch(ctx context.Context, scope service.Scope, done *journal, name string, raw []byte) (any, error) {
	switch name {
	case toolAdd:
		var args addArgs
		if err := decodeArgs(raw, &args); err != nil {
			return nil, err
		}
		return a.add(ctx, scope, done, args)
	case toolList:
		var args listArgs
		if err := decodeArgs(raw, &args); err != nil {
			return nil, err
		}
		return a.list(ctx, scope, args)
	case toolSum:
		var args periodArgs
		if err := decodeArgs(raw, &args); err != nil {
			return nil, err
		}
		return a.sum(ctx, scope, args)
	case toolUpdate:
		var args updateArgs
		if err := decodeArgs(raw, &args); err != nil {
			return nil, err
		}
		return a.update(ctx, scope, done, args)
	case toolDelete:
		var args deleteArgs
		if err := decodeArgs(raw, &args); err != nil {
			return nil, err
		}
		return a.delete(ctx, scope, done, args)
	default:
		return nil, toolError{fmt.Sprintf("неизвестный инструмент %q", name)}
	}
}

func (a *Agent) add(ctx context.Context, scope service.Scope, done *journal, args addArgs) (any, error) {
	amount, err := domain.MoneyFromFloat(float64(args.Amount))
	if err != nil {
		return nil, toolError{"некорректная сумма"}
	}
	in := service.NewExpense{Amount: amount, Category: domain.Category(args.Category), Comment: args.Comment}
	if args.SpentAt != "" {
		if in.SpentAt, err = parseDateArg("spent_at", args.SpentAt); err != nil {
			return nil, err
		}
	}
	e, err := a.expenses.Create(ctx, scope, in)
	if err != nil {
		return nil, err
	}
	done.record("записал", e)
	v := a.view(e)
	return mutationView{Added: &v}, nil
}

func (a *Agent) list(ctx context.Context, scope service.Scope, args listArgs) (any, error) {
	f, err := args.filter()
	if err != nil {
		return nil, err
	}
	f.Query = args.Query
	f.Sort = domain.SortField(args.Sort)
	f.Limit = min(max(int(args.Limit), 0), maxToolListLimit)
	if f.Limit == 0 {
		f.Limit = defaultToolListLimit
	}

	res, err := a.expenses.List(ctx, scope, f)
	if err != nil {
		return nil, err
	}
	out := listView{Items: make([]expenseView, 0, len(res.Items)), Count: res.Summary.Count, Total: res.Summary.Total.Float()}
	for _, e := range res.Items {
		out.Items = append(out.Items, a.view(e))
	}
	return out, nil
}

func (a *Agent) sum(ctx context.Context, scope service.Scope, args periodArgs) (any, error) {
	f, err := args.filter()
	if err != nil {
		return nil, err
	}
	s, err := a.expenses.Summary(ctx, scope, f)
	if err != nil {
		return nil, err
	}
	out := summaryView{Count: s.Count, Total: s.Total.Float()}
	for _, t := range s.ByCategory {
		out.ByCategory = append(out.ByCategory, categoryTotalView{
			Category: t.Category.Label(), Amount: t.Amount.Float(), Count: t.Count,
		})
	}
	return out, nil
}

func (a *Agent) update(ctx context.Context, scope service.Scope, done *journal, args updateArgs) (any, error) {
	id, err := parseID(args.ID)
	if err != nil {
		return nil, err
	}
	var p domain.ExpensePatch
	if args.Amount != 0 {
		m, err := domain.MoneyFromFloat(float64(args.Amount))
		if err != nil {
			return nil, toolError{"некорректная сумма"}
		}
		p.Amount = &m
	}
	if args.Category != "" {
		c := domain.Category(args.Category)
		p.Category = &c
	}
	if args.SpentAt != "" {
		d, err := parseDateArg("spent_at", args.SpentAt)
		if err != nil {
			return nil, err
		}
		p.SpentAt = &d
	}
	if c := strings.TrimSpace(args.Comment); c != "" {
		p.Comment = &c
	}
	if p.Empty() {
		return nil, toolError{"не передано ни одного изменяемого поля"}
	}

	e, err := a.expenses.Update(ctx, scope, id, p)
	if err != nil {
		return nil, err
	}
	done.record("изменил", e)
	v := a.view(e)
	return mutationView{Updated: &v}, nil
}

func (a *Agent) delete(ctx context.Context, scope service.Scope, done *journal, args deleteArgs) (any, error) {
	id, err := parseID(args.ID)
	if err != nil {
		return nil, err
	}
	// Удалённая трата возвращается, чтобы модель могла назвать, что именно удалено.
	e, err := a.expenses.Delete(ctx, scope, id)
	if err != nil {
		return nil, err
	}
	done.record("удалил", e)
	v := a.view(e)
	return mutationView{Deleted: &v}, nil
}

func (a *Agent) view(e domain.Expense) expenseView {
	return expenseView{
		ID:       e.ID.String(),
		Amount:   e.Amount.Float(),
		Category: e.Category.Label(),
		SpentAt:  e.SpentAt.Format(domain.DateLayout),
		Comment:  e.Comment,
		AddedAt:  e.CreatedAt.In(a.loc).Format(time.DateTime),
	}
}

func (p periodArgs) filter() (domain.ExpenseFilter, error) {
	var f domain.ExpenseFilter
	if p.From != "" {
		d, err := parseDateArg("from", p.From)
		if err != nil {
			return f, err
		}
		f.From = &d
	}
	if p.To != "" {
		d, err := parseDateArg("to", p.To)
		if err != nil {
			return f, err
		}
		f.To = &d
	}
	if p.Category != "" {
		c := domain.Category(p.Category)
		f.Category = &c
	}
	return f, nil
}

func decodeArgs(raw []byte, dst any) error {
	if len(raw) == 0 {
		raw = []byte("{}")
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return toolError{"некорректные аргументы: " + err.Error()}
	}
	return nil
}

func parseDateArg(field, v string) (time.Time, error) {
	d, err := domain.ParseDate(v)
	if err != nil {
		return time.Time{}, toolError{field + ": ожидается дата YYYY-MM-DD"}
	}
	return d, nil
}

func parseID(v string) (uuid.UUID, error) {
	id, err := uuid.Parse(v)
	if err != nil {
		return uuid.Nil, toolError{"некорректный id — возьми id из list_expenses"}
	}
	return id, nil
}
