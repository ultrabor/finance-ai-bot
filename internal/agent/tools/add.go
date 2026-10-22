package tools

import (
	"context"

	"finance-ai-bot/internal/domain"
	"finance-ai-bot/internal/usecase/expense"
)

const AddExpense = "add_expense"

type addArgs struct {
	Amount   flexNumber `json:"amount"`
	Category string     `json:"category"`
	SpentAt  string     `json:"spent_at"`
	Comment  string     `json:"comment"`
}

func (s *Set) addTool() tool {
	return tool{
		def: definition(AddExpense, "Записать новую трату пользователя.", `{
			"type": "object",
			"properties": {
				"amount":   {"type": "number", "description": "Сумма в рублях, больше 0"},
				"category": {"type": "string", "enum": [`+categoryEnum+`]},
				"spent_at": {"type": "string", "description": "Дата траты YYYY-MM-DD; по умолчанию сегодня"},
				"comment":  {"type": "string", "description": "Короткое описание: «кофе», «такси до работы»"}
			},
			"required": ["amount", "category"]
		}`),
		run: handle(s.add),
	}
}

func (s *Set) add(ctx context.Context, turn Turn, args addArgs) (any, error) {
	amount, err := parseAmount(args.Amount)
	if err != nil {
		return nil, err
	}
	in := expense.CreateParams{Amount: amount, Category: domain.Category(args.Category), Comment: args.Comment}
	if args.SpentAt != "" {
		if in.SpentAt, err = parseDateArg("spent_at", args.SpentAt); err != nil {
			return nil, err
		}
	}
	e, err := s.expenses.Create(ctx, turn.Scope, in)
	if err != nil {
		return nil, err
	}
	turn.Journal.Record("записал", e)
	return mutationView{Added: s.view(e)}, nil
}
