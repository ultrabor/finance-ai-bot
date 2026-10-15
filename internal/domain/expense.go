package domain

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// DateLayout — формат даты траты в API и в инструментах агента.
const DateLayout = time.DateOnly

var ErrNotFound = errors.New("not found")

// ValidationError — ошибка входных данных, понятная клиенту.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

func NewValidationError(field, message string) error {
	return &ValidationError{Field: field, Message: message}
}

// Expense — трата члена семьи.
type Expense struct {
	ID       uuid.UUID
	UserID   int64
	Amount   Money
	Category Category
	// SpentAt — календарная дата траты (полночь UTC).
	SpentAt   time.Time
	Comment   string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ExpensePatch — частичное изменение траты; nil-поля не меняются.
type ExpensePatch struct {
	Amount   *Money
	Category *Category
	SpentAt  *time.Time
	Comment  *string
}

func (p ExpensePatch) Empty() bool {
	return p.Amount == nil && p.Category == nil && p.SpentAt == nil && p.Comment == nil
}

// SortField — поле сортировки списка трат (всегда по убыванию).
type SortField string

const (
	SortBySpentAt   SortField = "spent_at"
	SortByCreatedAt SortField = "created_at"
)

// ExpenseFilter — условия выборки. Пустые поля не ограничивают выборку.
type ExpenseFilter struct {
	UserID   *int64
	From     *time.Time // включительно
	To       *time.Time // включительно
	Category *Category
	Query    string // подстрока комментария
	Sort     SortField
	Limit    int
	Offset   int
}

// CategoryTotal — итог по одной категории.
type CategoryTotal struct {
	Category Category
	Amount   Money
	Count    int
}

// ExpenseSummary — агрегаты по выборке.
type ExpenseSummary struct {
	Count      int
	Total      Money
	ByCategory []CategoryTotal
}

// ParseDate разбирает дату YYYY-MM-DD в полночь UTC.
func ParseDate(s string) (time.Time, error) {
	return time.Parse(DateLayout, s)
}

// TruncateDate отбрасывает время и часовой пояс, сохраняя календарную дату t.
func TruncateDate(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// DateOf возвращает календарную дату момента t в часовом поясе loc как полночь UTC.
func DateOf(t time.Time, loc *time.Location) time.Time {
	y, m, d := t.In(loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}
