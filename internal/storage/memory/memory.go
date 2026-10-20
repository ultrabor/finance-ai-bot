// Package memory — потокобезопасное хранилище в памяти для тестов и eval-прогонов агента.
package memory

import (
	"cmp"
	"context"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"finance-ai-bot/internal/domain"
)

type Store struct {
	mu       sync.Mutex
	expenses map[uuid.UUID]domain.Expense
	users    map[int64]domain.User
	now      func() time.Time
}

func New() *Store {
	return &Store{
		expenses: map[uuid.UUID]domain.Expense{},
		users:    map[int64]domain.User{},
		now:      time.Now,
	}
}

// Expenses возвращает репозиторий трат поверх хранилища.
func (s *Store) Expenses() *ExpenseRepo { return &ExpenseRepo{s} }

// Users возвращает репозиторий членов семьи поверх хранилища.
func (s *Store) Users() *UserRepo { return &UserRepo{s} }

type ExpenseRepo struct{ s *Store }

type UserRepo struct{ s *Store }

func (r *ExpenseRepo) Create(_ context.Context, e domain.Expense) (domain.Expense, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if _, ok := r.s.users[e.UserID]; !ok {
		return domain.Expense{}, domain.NewValidationError("user_id", "член семьи не найден")
	}
	e.ID = uuid.New()
	// Монотонное время добавления: сортировка по created_at в тестах должна быть стабильной.
	e.CreatedAt = r.s.now()
	for _, other := range r.s.expenses {
		if !e.CreatedAt.After(other.CreatedAt) {
			e.CreatedAt = other.CreatedAt.Add(time.Microsecond)
		}
	}
	e.UpdatedAt = e.CreatedAt
	r.s.expenses[e.ID] = e
	return e, nil
}

func (r *ExpenseRepo) Get(_ context.Context, id uuid.UUID, ownerID *int64) (domain.Expense, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	e, ok := r.s.expenses[id]
	if !ok || (ownerID != nil && e.UserID != *ownerID) {
		return domain.Expense{}, domain.ErrNotFound
	}
	return e, nil
}

func (r *ExpenseRepo) List(_ context.Context, f domain.ExpenseFilter) ([]domain.Expense, domain.ExpenseSummary, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	sum := r.s.summarize(f)
	items := r.s.filter(f)
	slices.SortFunc(items, func(a, b domain.Expense) int {
		if f.Sort != domain.SortByCreatedAt {
			if c := b.SpentAt.Compare(a.SpentAt); c != 0 {
				return c
			}
		}
		return b.CreatedAt.Compare(a.CreatedAt)
	})
	if f.Offset >= len(items) {
		return []domain.Expense{}, sum, nil
	}
	items = items[f.Offset:]
	if f.Limit > 0 && len(items) > f.Limit {
		items = items[:f.Limit]
	}
	return items, sum, nil
}

func (r *ExpenseRepo) Summary(_ context.Context, f domain.ExpenseFilter) (domain.ExpenseSummary, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	return r.s.summarize(f), nil
}

func (s *Store) summarize(f domain.ExpenseFilter) domain.ExpenseSummary {
	byCat := map[domain.Category]*domain.CategoryTotal{}
	var sum domain.ExpenseSummary
	for _, e := range s.filter(f) {
		t, ok := byCat[e.Category]
		if !ok {
			t = &domain.CategoryTotal{Category: e.Category}
			byCat[e.Category] = t
		}
		t.Amount += e.Amount
		t.Count++
		sum.Total += e.Amount
		sum.Count++
	}
	for _, t := range byCat {
		sum.ByCategory = append(sum.ByCategory, *t)
	}
	slices.SortFunc(sum.ByCategory, func(a, b domain.CategoryTotal) int {
		return cmp.Or(cmp.Compare(b.Amount, a.Amount), cmp.Compare(a.Category, b.Category))
	})
	return sum
}

func (r *ExpenseRepo) Update(_ context.Context, id uuid.UUID, ownerID *int64, p domain.ExpensePatch) (domain.Expense, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	e, ok := r.s.expenses[id]
	if !ok || (ownerID != nil && e.UserID != *ownerID) {
		return domain.Expense{}, domain.ErrNotFound
	}
	if p.Amount != nil {
		e.Amount = *p.Amount
	}
	if p.Category != nil {
		e.Category = *p.Category
	}
	if p.SpentAt != nil {
		e.SpentAt = *p.SpentAt
	}
	if p.Comment != nil {
		e.Comment = *p.Comment
	}
	e.UpdatedAt = r.s.now()
	r.s.expenses[id] = e
	return e, nil
}

func (r *ExpenseRepo) Delete(_ context.Context, id uuid.UUID, ownerID *int64) (domain.Expense, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	e, ok := r.s.expenses[id]
	if !ok || (ownerID != nil && e.UserID != *ownerID) {
		return domain.Expense{}, domain.ErrNotFound
	}
	delete(r.s.expenses, id)
	return e, nil
}

func (r *UserRepo) Upsert(_ context.Context, u domain.User) (domain.User, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if old, ok := r.s.users[u.TelegramID]; ok {
		u.CreatedAt = old.CreatedAt
	} else {
		u.CreatedAt = r.s.now()
	}
	r.s.users[u.TelegramID] = u
	return u, nil
}

func (r *UserRepo) List(_ context.Context) ([]domain.User, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	out := make([]domain.User, 0, len(r.s.users))
	for _, u := range r.s.users {
		out = append(out, u)
	}
	// Тот же порядок, что в postgres.UserRepo.List.
	slices.SortFunc(out, func(a, b domain.User) int {
		return cmp.Or(
			cmp.Compare(strings.ToLower(a.FirstName), strings.ToLower(b.FirstName)),
			cmp.Compare(a.TelegramID, b.TelegramID),
		)
	})
	return out, nil
}

func (s *Store) filter(f domain.ExpenseFilter) []domain.Expense {
	var out []domain.Expense
	q := strings.ToLower(f.Query)
	for _, e := range s.expenses {
		switch {
		case f.UserID != nil && e.UserID != *f.UserID,
			f.From != nil && e.SpentAt.Before(*f.From),
			f.To != nil && e.SpentAt.After(*f.To),
			f.Category != nil && e.Category != *f.Category,
			q != "" && !strings.Contains(strings.ToLower(e.Comment), q):
			continue
		}
		out = append(out, e)
	}
	return out
}
