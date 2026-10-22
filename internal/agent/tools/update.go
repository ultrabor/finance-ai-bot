package tools

import (
	"context"
	"strings"

	"finance-ai-bot/internal/domain"
)

const UpdateExpense = "update_expense"

// updateArgs: пустые значения ("" и 0) означают «не менять» — модели часто присылают все поля схемы.
type updateArgs struct {
	ID       string     `json:"id"`
	Amount   flexNumber `json:"amount"`
	Category string     `json:"category"`
	SpentAt  string     `json:"spent_at"`
	Comment  string     `json:"comment"`
}

func (s *Set) updateTool() tool {
	return tool{
		def: definition(UpdateExpense, "Изменить трату пользователя. Передавай только изменяемые поля; id бери из list_expenses.", `{
			"type": "object",
			"properties": {
				"id":       {"type": "string", "description": "id траты из list_expenses"},
				"amount":   {"type": "number", "description": "Новая сумма в рублях"},
				"category": {"type": "string", "enum": [`+categoryEnum+`]},
				"spent_at": {"type": "string", "description": "Новая дата YYYY-MM-DD"},
				"comment":  {"type": "string", "description": "Новое описание; не передавай, если описание не меняется"}
			},
			"required": ["id"]
		}`),
		run: handle(s.update),
	}
}

func (s *Set) update(ctx context.Context, turn Turn, args updateArgs) (any, error) {
	id, err := parseID(args.ID)
	if err != nil {
		return nil, err
	}
	p, err := args.patch()
	if err != nil {
		return nil, err
	}
	e, err := s.expenses.Update(ctx, turn.Scope, id, p)
	if err != nil {
		return nil, err
	}
	turn.Journal.Record("изменил", e)
	return mutationView{Updated: s.view(e)}, nil
}

func (args updateArgs) patch() (domain.ExpensePatch, error) {
	var p domain.ExpensePatch
	if args.Amount != 0 {
		m, err := parseAmount(args.Amount)
		if err != nil {
			return p, err
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
			return p, err
		}
		p.SpentAt = &d
	}
	if c := strings.TrimSpace(args.Comment); c != "" {
		p.Comment = &c
	}
	if p.Empty() {
		return p, toolError{"не передано ни одного изменяемого поля"}
	}
	return p, nil
}
