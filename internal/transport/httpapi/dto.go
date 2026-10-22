package httpapi

import (
	"encoding/json"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"finance-ai-bot/internal/domain"
)

// Типы ответов и UserCreate генерируются из api/openapi.yaml в types.gen.go (make generate).
// Здесь — мапперы домена в них и тела, которые приходится разбирать вручную.

func toExpense(e domain.Expense) Expense {
	return Expense{
		ID:        e.ID,
		UserID:    e.UserID,
		Amount:    e.Amount.Float(),
		Category:  CategoryCode(e.Category),
		SpentAt:   openapi_types.Date{Time: e.SpentAt},
		Comment:   nullable(e.Comment),
		CreatedAt: e.CreatedAt,
		UpdatedAt: e.UpdatedAt,
	}
}

func toUser(u domain.User) User {
	return User{
		TelegramID:  u.TelegramID,
		Username:    nullable(u.Username),
		FirstName:   u.FirstName,
		LastName:    nullable(u.LastName),
		DisplayName: u.DisplayName(),
		CreatedAt:   u.CreatedAt,
	}
}

// Тела ExpenseCreate и ExpenseUpdate разбираются вручную: сгенерированные типы
// не отличают строку "350" от числа, теряют лишние знаки суммы при разборе во float64
// и не различают отсутствующее поле и null. Набор полей сверяется со спекой в тестах.

// createExpenseRequest — ExpenseCreate; сумма сырым JSON, чтобы отклонять строки.
type createExpenseRequest struct {
	UserID   int64           `json:"user_id"`
	Amount   json.RawMessage `json:"amount"`
	Category string          `json:"category"`
	SpentAt  string          `json:"spent_at"`
	Comment  *string         `json:"comment"`
}

// updateExpenseRequest — ExpenseUpdate; отсутствующее поле (nil) отличается от явного null.
type updateExpenseRequest struct {
	Amount   json.RawMessage `json:"amount"`
	Category json.RawMessage `json:"category"`
	SpentAt  json.RawMessage `json:"spent_at"`
	Comment  json.RawMessage `json:"comment"`
}

func apiError(code ErrorCode, msg string) Error {
	return Error{Error: ErrorBody{Code: code, Message: msg}}
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
