package tools

import "context"

const SumExpenses = "sum_expenses"

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

func (s *Set) sumTool() tool {
	return tool{
		def: definition(SumExpenses),
		run: handle(s.sum),
	}
}

func (s *Set) sum(ctx context.Context, turn Turn, args periodArgs) (any, error) {
	f, err := args.filter()
	if err != nil {
		return nil, err
	}
	sum, err := s.expenses.Summary(ctx, turn.Scope, f)
	if err != nil {
		return nil, err
	}
	out := summaryView{Count: sum.Count, Total: sum.Total.Float()}
	for _, t := range sum.ByCategory {
		out.ByCategory = append(out.ByCategory, categoryTotalView{
			Category: t.Category.Label(), Amount: t.Amount.Float(), Count: t.Count,
		})
	}
	return out, nil
}
