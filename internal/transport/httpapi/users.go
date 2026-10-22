package httpapi

import (
	"net/http"
	"strings"

	"finance-ai-bot/internal/domain"
)

func (h *Handler) listUsers(w http.ResponseWriter, r *http.Request) {
	users, err := h.users.List(r.Context())
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	items := make([]User, 0, len(users))
	for _, u := range users {
		items = append(items, toUser(u))
	}
	writeJSON(w, http.StatusOK, UserList{Items: items})
}

func (h *Handler) createUser(w http.ResponseWriter, r *http.Request) {
	var req UserCreate
	if err := decodeJSON(w, r, &req); err != nil {
		h.writeError(w, r, err)
		return
	}
	// Имя обязательно только при ручном добавлении: у пользователя Telegram оно может
	// состоять из невидимых символов, и бот не должен из-за этого отказывать.
	if strings.TrimSpace(req.FirstName) == "" {
		h.writeError(w, r, domain.NewValidationError("first_name", "укажите имя"))
		return
	}
	u, err := h.users.Upsert(r.Context(), domain.User{
		TelegramID: req.TelegramID,
		Username:   deref(req.Username),
		FirstName:  req.FirstName,
		LastName:   deref(req.LastName),
	})
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toUser(u))
}
