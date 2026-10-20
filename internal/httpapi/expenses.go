package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/google/uuid"

	"finance-ai-bot/internal/domain"
	"finance-ai-bot/internal/service"
)

func (h *Handler) listExpenses(w http.ResponseWriter, r *http.Request) {
	f, err := parseExpenseFilter(r.URL.Query())
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	res, err := h.expenses.List(r.Context(), service.FamilyScope(), f)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	out := expenseListDTO{
		Items: make([]expenseDTO, 0, len(res.Items)),
		Total: res.Summary.Count,
		Sum:   res.Summary.Total.Float(),
	}
	for _, e := range res.Items {
		out.Items = append(out.Items, toExpenseDTO(e))
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) createExpense(w http.ResponseWriter, r *http.Request) {
	var req createExpenseRequest
	if err := decodeJSON(w, r, &req); err != nil {
		h.writeError(w, r, err)
		return
	}
	amount, err := parseAmount(req.Amount)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	in := service.NewExpense{
		UserID:   req.UserID,
		Amount:   amount,
		Category: domain.Category(req.Category),
	}
	if req.SpentAt == "" {
		h.writeError(w, r, domain.NewValidationError("spent_at", "укажите дату"))
		return
	}
	if in.SpentAt, err = parseDate("spent_at", req.SpentAt); err != nil {
		h.writeError(w, r, err)
		return
	}
	if req.Comment != nil {
		in.Comment = *req.Comment
	}

	e, err := h.expenses.Create(r.Context(), service.FamilyScope(), in)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toExpenseDTO(e))
}

func (h *Handler) getExpense(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	e, err := h.expenses.Get(r.Context(), service.FamilyScope(), id)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toExpenseDTO(e))
}

func (h *Handler) updateExpense(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	var req updateExpenseRequest
	if err := decodeJSON(w, r, &req); err != nil {
		h.writeError(w, r, err)
		return
	}

	p, err := req.patch()
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	e, err := h.expenses.Update(r.Context(), service.FamilyScope(), id, p)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toExpenseDTO(e))
}

func (h *Handler) deleteExpense(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	if _, err := h.expenses.Delete(r.Context(), service.FamilyScope(), id); err != nil {
		h.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (req updateExpenseRequest) patch() (domain.ExpensePatch, error) {
	var p domain.ExpensePatch
	if req.Amount != nil {
		amount, err := parseAmount(req.Amount)
		if err != nil {
			return p, err
		}
		p.Amount = &amount
	}
	if req.Category != nil {
		c, err := requiredString("category", req.Category)
		if err != nil {
			return p, err
		}
		cat := domain.Category(c)
		p.Category = &cat
	}
	if req.SpentAt != nil {
		v, err := requiredString("spent_at", req.SpentAt)
		if err != nil {
			return p, err
		}
		d, err := parseDate("spent_at", v)
		if err != nil {
			return p, err
		}
		p.SpentAt = &d
	}
	if req.Comment != nil {
		// null очищает комментарий так же, как пустая строка.
		var c string
		if !isNull(req.Comment) {
			if err := json.Unmarshal(req.Comment, &c); err != nil {
				return p, domain.NewValidationError("comment", "должен быть строкой")
			}
		}
		p.Comment = &c
	}
	return p, nil
}

// requiredString разбирает непустое строковое поле PATCH; null для него запрещён.
func requiredString(field string, raw json.RawMessage) (string, error) {
	var s string
	if isNull(raw) || json.Unmarshal(raw, &s) != nil {
		return "", domain.NewValidationError(field, "должно быть строкой, null не допускается")
	}
	return s, nil
}

func isNull(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

func parseExpenseFilter(q url.Values) (domain.ExpenseFilter, error) {
	var f domain.ExpenseFilter
	if v := q.Get("user_id"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return f, domain.NewValidationError("user_id", "должен быть числом")
		}
		f.UserID = &id
	}
	for _, p := range []struct {
		name string
		dst  **time.Time
	}{{"from", &f.From}, {"to", &f.To}} {
		if v := q.Get(p.name); v != "" {
			d, err := parseDate(p.name, v)
			if err != nil {
				return f, err
			}
			*p.dst = &d
		}
	}
	if v := q.Get("category"); v != "" {
		c := domain.Category(v)
		f.Category = &c
	}
	f.Query = q.Get("q")

	var err error
	if f.Limit, err = intParam(q, "limit", service.DefaultListLimit); err != nil {
		return f, err
	}
	if f.Limit < 1 || f.Limit > service.MaxListLimit {
		return f, domain.NewValidationError("limit", fmt.Sprintf("от 1 до %d", service.MaxListLimit))
	}
	if f.Offset, err = intParam(q, "offset", 0); err != nil {
		return f, err
	}
	return f, nil
}

func intParam(q url.Values, name string, def int) (int, error) {
	v := q.Get(name)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 0, domain.NewValidationError(name, "должен быть неотрицательным числом")
	}
	return n, nil
}

func parseDate(field, v string) (time.Time, error) {
	d, err := domain.ParseDate(v)
	if err != nil {
		return time.Time{}, domain.NewValidationError(field, "дата в формате YYYY-MM-DD")
	}
	return d, nil
}

// parseAmount принимает только положительное JSON-число с не более чем двумя знаками после точки.
func parseAmount(raw json.RawMessage) (domain.Money, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] == '"' || isNull(raw) {
		return 0, domain.NewValidationError("amount", "сумма должна быть числом")
	}
	m, err := domain.ParseMoney(string(raw))
	if errors.Is(err, domain.ErrMoneyTooLarge) {
		return 0, domain.NewValidationError("amount", "слишком большая сумма (не больше 100 000 000 ₽)")
	}
	if err != nil {
		return 0, domain.NewValidationError("amount", "сумма должна быть положительным числом, не более двух знаков после точки")
	}
	return m, nil
}

func pathID(r *http.Request) (uuid.UUID, error) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		// Невалидный UUID не может существовать — отвечаем как на отсутствующую трату.
		return uuid.Nil, domain.ErrNotFound
	}
	return id, nil
}
