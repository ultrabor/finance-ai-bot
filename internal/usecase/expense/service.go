// Package expense — сценарии учёта трат: валидация и разграничение доступа.
package expense

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"finance-ai-bot/internal/domain"
)

const (
	DefaultListLimit = 50
	MaxListLimit     = 500
	MaxCommentLen    = 500
)

type Service struct {
	repo Repository
	loc  *time.Location
	now  func() time.Time
}

func NewService(repo Repository, loc *time.Location) *Service {
	return &Service{repo: repo, loc: loc, now: time.Now}
}

// CreateParams — данные для создания траты. Нулевой SpentAt означает «сегодня».
type CreateParams struct {
	UserID   int64
	Amount   domain.Money
	Category domain.Category
	SpentAt  time.Time
	Comment  string
}

// ListResult — страница трат и агрегаты по всей выборке.
type ListResult struct {
	Items   []domain.Expense
	Summary domain.ExpenseSummary
}

func (s *Service) Create(ctx context.Context, scope Scope, in CreateParams) (domain.Expense, error) {
	owner, err := scope.owner()
	if err != nil {
		return domain.Expense{}, err
	}
	if owner != nil {
		in.UserID = *owner
	}
	if in.UserID <= 0 {
		return domain.Expense{}, domain.NewValidationError("user_id", "укажите члена семьи")
	}
	if in.SpentAt.IsZero() {
		in.SpentAt = s.Today()
	}
	in.SpentAt = domain.TruncateDate(in.SpentAt)
	in.Comment = strings.TrimSpace(in.Comment)

	if err := s.validateAmount(in.Amount); err != nil {
		return domain.Expense{}, err
	}
	if err := validateCategory(in.Category); err != nil {
		return domain.Expense{}, err
	}
	if err := s.validateDate(in.SpentAt); err != nil {
		return domain.Expense{}, err
	}
	if err := validateComment(in.Comment); err != nil {
		return domain.Expense{}, err
	}

	return s.repo.Create(ctx, domain.Expense{
		UserID:   in.UserID,
		Amount:   in.Amount,
		Category: in.Category,
		SpentAt:  in.SpentAt,
		Comment:  in.Comment,
	})
}

func (s *Service) Get(ctx context.Context, scope Scope, id uuid.UUID) (domain.Expense, error) {
	owner, err := scope.owner()
	if err != nil {
		return domain.Expense{}, err
	}
	return s.repo.Get(ctx, id, owner)
}

func (s *Service) List(ctx context.Context, scope Scope, f domain.ExpenseFilter) (ListResult, error) {
	f, err := s.prepareFilter(scope, f)
	if err != nil {
		return ListResult{}, err
	}
	if f.Limit <= 0 {
		f.Limit = DefaultListLimit
	}
	if f.Limit > MaxListLimit {
		f.Limit = MaxListLimit
	}
	if f.Offset < 0 {
		return ListResult{}, domain.NewValidationError("offset", "не может быть отрицательным")
	}
	if f.Sort == "" {
		f.Sort = domain.SortBySpentAt
	}
	if f.Sort != domain.SortBySpentAt && f.Sort != domain.SortByCreatedAt {
		return ListResult{}, domain.NewValidationError("sort", "неизвестное поле сортировки")
	}

	items, sum, err := s.repo.List(ctx, f)
	if err != nil {
		return ListResult{}, err
	}
	return ListResult{Items: items, Summary: sum}, nil
}

func (s *Service) Summary(ctx context.Context, scope Scope, f domain.ExpenseFilter) (domain.ExpenseSummary, error) {
	f, err := s.prepareFilter(scope, f)
	if err != nil {
		return domain.ExpenseSummary{}, err
	}
	return s.repo.Summary(ctx, f)
}

func (s *Service) Update(ctx context.Context, scope Scope, id uuid.UUID, p domain.ExpensePatch) (domain.Expense, error) {
	owner, err := scope.owner()
	if err != nil {
		return domain.Expense{}, err
	}
	if p.Empty() {
		return domain.Expense{}, domain.NewValidationError("body", "нет полей для изменения")
	}
	if p.Amount != nil {
		if err := s.validateAmount(*p.Amount); err != nil {
			return domain.Expense{}, err
		}
	}
	if p.Category != nil {
		if err := validateCategory(*p.Category); err != nil {
			return domain.Expense{}, err
		}
	}
	if p.SpentAt != nil {
		d := domain.TruncateDate(*p.SpentAt)
		if err := s.validateDate(d); err != nil {
			return domain.Expense{}, err
		}
		p.SpentAt = &d
	}
	if p.Comment != nil {
		c := strings.TrimSpace(*p.Comment)
		if err := validateComment(c); err != nil {
			return domain.Expense{}, err
		}
		p.Comment = &c
	}
	return s.repo.Update(ctx, id, owner, p)
}

// Delete удаляет трату и возвращает её, чтобы вызывающий мог назвать, что удалено.
func (s *Service) Delete(ctx context.Context, scope Scope, id uuid.UUID) (domain.Expense, error) {
	owner, err := scope.owner()
	if err != nil {
		return domain.Expense{}, err
	}
	return s.repo.Delete(ctx, id, owner)
}

// Today — текущая дата в часовом поясе семьи.
func (s *Service) Today() time.Time {
	return domain.DateOf(s.now(), s.loc)
}

// prepareFilter накладывает ограничение области доступа и проверяет период.
func (s *Service) prepareFilter(scope Scope, f domain.ExpenseFilter) (domain.ExpenseFilter, error) {
	owner, err := scope.owner()
	if err != nil {
		return f, err
	}
	if owner != nil {
		f.UserID = owner
	}
	if f.Category != nil {
		if err := validateCategory(*f.Category); err != nil {
			return f, err
		}
	}
	for _, d := range []**time.Time{&f.From, &f.To} {
		if *d != nil {
			t := domain.TruncateDate(**d)
			*d = &t
		}
	}
	if f.From != nil && f.To != nil && f.From.After(*f.To) {
		return f, domain.NewValidationError("from", "начало периода позже конца")
	}
	f.Query = strings.TrimSpace(f.Query)
	return f, nil
}

func (s *Service) validateAmount(m domain.Money) error {
	if m <= 0 {
		return domain.NewValidationError("amount", "сумма должна быть больше нуля")
	}
	if m > domain.MaxMoney {
		return domain.NewValidationError("amount", "слишком большая сумма")
	}
	return nil
}

func (s *Service) validateDate(d time.Time) error {
	if d.IsZero() {
		return domain.NewValidationError("spent_at", "укажите дату")
	}
	// Допускаем один день вперёд: у членов семьи могут быть разные часовые пояса.
	if d.After(s.Today().AddDate(0, 0, 1)) {
		return domain.NewValidationError("spent_at", "дата траты не может быть в будущем")
	}
	if d.Year() < 2000 {
		return domain.NewValidationError("spent_at", "слишком ранняя дата")
	}
	return nil
}

func validateCategory(c domain.Category) error {
	if !c.Valid() {
		return domain.NewValidationError("category", "неизвестная категория")
	}
	return nil
}

func validateComment(c string) error {
	if utf8.RuneCountInString(c) > MaxCommentLen {
		return domain.NewValidationError("comment", "комментарий длиннее 500 символов")
	}
	return nil
}
