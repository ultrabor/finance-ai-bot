package tools

import (
	"time"

	"finance-ai-bot/internal/domain"
)

// Представления результатов для модели: суммы в рублях, категории по-русски.

type expenseView struct {
	ID       string  `json:"id"`
	Amount   float64 `json:"amount"`
	Category string  `json:"category"`
	SpentAt  string  `json:"spent_at"`
	Comment  string  `json:"comment,omitempty"`
	AddedAt  string  `json:"added_at"`
}

// mutationView — результат изменяющего инструмента.
type mutationView struct {
	Added   *expenseView `json:"added,omitempty"`
	Updated *expenseView `json:"updated,omitempty"`
	Deleted *expenseView `json:"deleted,omitempty"`
}

func (s *Set) view(e domain.Expense) *expenseView {
	return &expenseView{
		ID:       e.ID.String(),
		Amount:   e.Amount.Float(),
		Category: e.Category.Label(),
		SpentAt:  e.SpentAt.Format(domain.DateLayout),
		Comment:  e.Comment,
		AddedAt:  e.CreatedAt.In(s.loc).Format(time.DateTime),
	}
}
