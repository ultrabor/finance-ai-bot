package httpapi

import (
	"encoding/json"
	"time"

	"finance-ai-bot/internal/domain"
)

type expenseDTO struct {
	ID        string    `json:"id"`
	UserID    int64     `json:"user_id"`
	Amount    float64   `json:"amount"`
	Category  string    `json:"category"`
	SpentAt   string    `json:"spent_at"`
	Comment   *string   `json:"comment"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func toExpenseDTO(e domain.Expense) expenseDTO {
	dto := expenseDTO{
		ID:        e.ID.String(),
		UserID:    e.UserID,
		Amount:    e.Amount.Float(),
		Category:  string(e.Category),
		SpentAt:   e.SpentAt.Format(domain.DateLayout),
		CreatedAt: e.CreatedAt,
		UpdatedAt: e.UpdatedAt,
	}
	if e.Comment != "" {
		dto.Comment = &e.Comment
	}
	return dto
}

type expenseListDTO struct {
	Items []expenseDTO `json:"items"`
	Total int          `json:"total"`
	Sum   float64      `json:"sum"`
}

// Сумма принимается сырым JSON, чтобы отличать число от строки ("350" отклоняется).
type createExpenseRequest struct {
	UserID   int64           `json:"user_id"`
	Amount   json.RawMessage `json:"amount"`
	Category string          `json:"category"`
	SpentAt  string          `json:"spent_at"`
	Comment  *string         `json:"comment"`
}

// updateExpenseRequest различает отсутствующее поле (nil) и явный null через json.RawMessage.
type updateExpenseRequest struct {
	Amount   json.RawMessage `json:"amount"`
	Category json.RawMessage `json:"category"`
	SpentAt  json.RawMessage `json:"spent_at"`
	Comment  json.RawMessage `json:"comment"`
}

type userDTO struct {
	TelegramID  int64     `json:"telegram_id"`
	Username    *string   `json:"username"`
	FirstName   string    `json:"first_name"`
	LastName    *string   `json:"last_name"`
	DisplayName string    `json:"display_name"`
	CreatedAt   time.Time `json:"created_at"`
}

func toUserDTO(u domain.User) userDTO {
	return userDTO{
		TelegramID:  u.TelegramID,
		Username:    nullable(u.Username),
		FirstName:   u.FirstName,
		LastName:    nullable(u.LastName),
		DisplayName: u.DisplayName(),
		CreatedAt:   u.CreatedAt,
	}
}

type createUserRequest struct {
	TelegramID int64  `json:"telegram_id"`
	FirstName  string `json:"first_name"`
	LastName   string `json:"last_name"`
	Username   string `json:"username"`
}

type categoryDTO struct {
	Code  string `json:"code"`
	Label string `json:"label"`
}

type itemsResponse[T any] struct {
	Items []T `json:"items"`
}

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Field   string `json:"field,omitempty"`
}

type errorResponse struct {
	Error errorBody `json:"error"`
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
