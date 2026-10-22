// Package httpapi — REST API веб-панели главы семьи.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"finance-ai-bot/internal/domain"
)

const maxBodySize = 64 << 10

type Handler struct {
	expenses ExpenseUseCase
	users    UserUseCase
	db       Pinger
	log      *slog.Logger
}

func New(expenses ExpenseUseCase, users UserUseCase, db Pinger, log *slog.Logger) *Handler {
	return &Handler{expenses: expenses, users: users, db: db, log: log}
}

func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", h.health)

	mux.HandleFunc("GET /api/v1/expenses", h.listExpenses)
	mux.HandleFunc("POST /api/v1/expenses", h.createExpense)
	mux.HandleFunc("GET /api/v1/expenses/{id}", h.getExpense)
	mux.HandleFunc("PATCH /api/v1/expenses/{id}", h.updateExpense)
	mux.HandleFunc("DELETE /api/v1/expenses/{id}", h.deleteExpense)

	mux.HandleFunc("GET /api/v1/users", h.listUsers)
	mux.HandleFunc("POST /api/v1/users", h.createUser)

	mux.HandleFunc("GET /api/v1/categories", h.listCategories)

	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { notRouted(mux, w, r) })

	return h.recoverer(h.logRequests(mux))
}

// notRouted отвечает JSON-ошибкой на запрос, для которого нет обработчика:
// 405 с заголовком Allow, если путь известен для других методов, иначе 404.
func notRouted(mux *http.ServeMux, w http.ResponseWriter, r *http.Request) {
	var allowed []string
	for _, m := range []string{http.MethodGet, http.MethodPost, http.MethodPatch, http.MethodDelete} {
		probe := r.Clone(r.Context())
		probe.Method = m
		if _, pattern := mux.Handler(probe); pattern != "/api/" && pattern != "" {
			allowed = append(allowed, m)
			// GET-шаблоны mux обслуживает и для HEAD — как в его собственном 405.
			if m == http.MethodGet {
				allowed = append(allowed, http.MethodHead)
			}
		}
	}
	if len(allowed) == 0 {
		writeJSON(w, http.StatusNotFound, errorResponse{Error: errorBody{Code: "not_found", Message: "маршрут не найден"}})
		return
	}
	w.Header().Set("Allow", strings.Join(allowed, ", "))
	writeJSON(w, http.StatusMethodNotAllowed, errorResponse{Error: errorBody{
		Code: "method_not_allowed", Message: "метод не поддерживается для этого пути",
	}})
}

func (h *Handler) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := h.db.Ping(ctx); err != nil {
		h.log.WarnContext(ctx, "health check failed", "error", err)
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) listCategories(w http.ResponseWriter, _ *http.Request) {
	items := make([]categoryDTO, 0, len(domain.Categories))
	for _, c := range domain.Categories {
		items = append(items, categoryDTO{Code: string(c), Label: c.Label()})
	}
	writeJSON(w, http.StatusOK, itemsResponse[categoryDTO]{Items: items})
}

// errBadRequest — ошибка разбора запроса (не путать с бизнес-валидацией).
type errBadRequest struct{ msg string }

func (e errBadRequest) Error() string { return e.msg }

func badRequest(format string, args ...any) error {
	return errBadRequest{msg: fmt.Sprintf(format, args...)}
}

func (h *Handler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	var (
		validation *domain.ValidationError
		bad        errBadRequest
		tooLarge   *http.MaxBytesError
	)
	switch {
	case errors.As(err, &tooLarge):
		writeJSON(w, http.StatusRequestEntityTooLarge, errorResponse{Error: errorBody{
			Code: "payload_too_large", Message: "слишком большое тело запроса",
		}})
	case errors.As(err, &validation):
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: errorBody{
			Code: "validation_error", Message: validation.Message, Field: validation.Field,
		}})
	case errors.As(err, &bad):
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: errorBody{Code: "bad_request", Message: bad.msg}})
	case errors.Is(err, domain.ErrNotFound):
		writeJSON(w, http.StatusNotFound, errorResponse{Error: errorBody{Code: "not_found", Message: "не найдено"}})
	default:
		h.log.ErrorContext(r.Context(), "request failed", "method", r.Method, "path", r.URL.Path, "error", err)
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: errorBody{
			Code: "internal_error", Message: "внутренняя ошибка сервера",
		}})
	}
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodySize))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		if errors.Is(err, io.EOF) {
			return badRequest("пустое тело запроса")
		}
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			return err
		}
		return badRequest("некорректный JSON: %v", err)
	}
	// После объекта не должно быть данных: `{"amount":5} мусор` — ошибка.
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			return err
		}
		return badRequest("лишние данные после JSON-объекта")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (h *Handler) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		if r.URL.Path == "/healthz" {
			return
		}
		h.log.InfoContext(r.Context(), "http request",
			"method", r.Method, "path", r.URL.Path, "status", rec.status, "duration", time.Since(start))
	})
}

func (h *Handler) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				h.log.ErrorContext(r.Context(), "panic", "value", v, "stack", string(debug.Stack()))
				writeJSON(w, http.StatusInternalServerError, errorResponse{Error: errorBody{
					Code: "internal_error", Message: "внутренняя ошибка сервера",
				}})
			}
		}()
		next.ServeHTTP(w, r)
	})
}
