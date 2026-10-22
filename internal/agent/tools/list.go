package tools

import (
	"context"

	"finance-ai-bot/internal/domain"
)

const (
	ListExpenses = "list_expenses"

	defaultListLimit = 10
	maxListLimit     = 50
)

type listArgs struct {
	periodArgs
	Query string     `json:"query"`
	Sort  string     `json:"sort"`
	Limit flexNumber `json:"limit"`
}

type listView struct {
	Items []*expenseView `json:"items"`
	Count int            `json:"count"`
	Total float64        `json:"total"`
}

func (s *Set) listTool() tool {
	return tool{
		def: definition(ListExpenses, "Найти траты пользователя. Возвращает до limit трат с id, а также количество и сумму всех найденных.", `{
			"type": "object",
			"properties": {
				"from":     {"type": "string", "description": "Дата с (включительно), YYYY-MM-DD"},
				"to":       {"type": "string", "description": "Дата по (включительно), YYYY-MM-DD"},
				"category": {"type": "string", "enum": [`+categoryEnum+`]},
				"query":    {"type": "string", "description": "Подстрока описания траты, например «такси»"},
				"sort":     {"type": "string", "enum": ["spent_at", "created_at"], "description": "spent_at — по дате траты (по умолчанию), created_at — по времени добавления; всегда от новых к старым"},
				"limit":    {"type": "integer", "description": "Сколько трат вернуть, 1–50, по умолчанию 10"}
			}
		}`),
		run: handle(s.list),
	}
}

func (s *Set) list(ctx context.Context, turn Turn, args listArgs) (any, error) {
	f, err := args.filter()
	if err != nil {
		return nil, err
	}
	f.Query = args.Query
	f.Sort = domain.SortField(args.Sort)
	f.Limit = min(max(int(args.Limit), 0), maxListLimit)
	if f.Limit == 0 {
		f.Limit = defaultListLimit
	}

	res, err := s.expenses.List(ctx, turn.Scope, f)
	if err != nil {
		return nil, err
	}
	out := listView{Items: make([]*expenseView, 0, len(res.Items)), Count: res.Summary.Count, Total: res.Summary.Total.Float()}
	for _, e := range res.Items {
		out.Items = append(out.Items, s.view(e))
	}
	return out, nil
}
