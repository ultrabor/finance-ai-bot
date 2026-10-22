package httpapi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"finance-ai-bot/internal/domain"
	"finance-ai-bot/internal/usecase/expense"
)

var errDB = errors.New("pq: connection refused 10.0.0.5")

// brokenExpenses — use case, у которого отказало хранилище; Get паникует.
type brokenExpenses struct{ ExpenseUseCase }

func (brokenExpenses) List(context.Context, expense.Scope, domain.ExpenseFilter) (expense.ListResult, error) {
	return expense.ListResult{}, errDB
}

func (brokenExpenses) Get(context.Context, expense.Scope, uuid.UUID) (domain.Expense, error) {
	panic("nil map")
}

type brokenUsers struct{}

func (brokenUsers) Upsert(context.Context, domain.User) (domain.User, error) {
	return domain.User{}, errDB
}
func (brokenUsers) List(context.Context) ([]domain.User, error) { return nil, errDB }

type downPinger struct{}

func (downPinger) Ping(context.Context) error { return errDB }

func newBrokenServer(t *testing.T) (*httptest.Server, *strings.Builder) {
	t.Helper()
	logs := &strings.Builder{}
	h := New(brokenExpenses{}, brokenUsers{}, downPinger{}, slog.New(slog.NewTextHandler(logs, nil)))
	srv := httptest.NewServer(h.Routes())
	t.Cleanup(srv.Close)
	return srv, logs
}

// Сбой зависимостей — 500 с общим сообщением; детали только в логе.
func TestInternalErrorsDoNotLeak(t *testing.T) {
	srv, logs := newBrokenServer(t)
	tests := []struct{ name, method, path, body string }{
		{"list expenses", "GET", "/api/v1/expenses", ""},
		{"list users", "GET", "/api/v1/users", ""},
		{"create user", "POST", "/api/v1/users", `{"telegram_id":5,"first_name":"Мама"}`},
		{"panic", "GET", "/api/v1/expenses/" + uuid.NewString(), ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var resp Error
			do(t, srv, tt.method, tt.path, tt.body, http.StatusInternalServerError, &resp)
			if resp.Error.Code != ErrorCodeInternalError || strings.Contains(resp.Error.Message, "10.0.0.5") {
				t.Errorf("500 body = %+v", resp.Error)
			}
		})
	}
	for _, want := range []string{"connection refused", "nil map"} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("log does not mention %q", want)
		}
	}
}

func TestHealthReportsDatabaseDown(t *testing.T) {
	srv, _ := newBrokenServer(t)
	resp, err := srv.Client().Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable || strings.Contains(string(body), "10.0.0.5") {
		t.Errorf("healthz = %d %q", resp.StatusCode, body)
	}
}

func TestPatchFields(t *testing.T) {
	srv := newServer(t)
	var created Expense
	do(t, srv, "POST", "/api/v1/expenses",
		`{"user_id":111,"amount":100,"category":"food","spent_at":"2026-01-01","comment":"обед"}`,
		http.StatusCreated, &created)
	path := "/api/v1/expenses/" + created.ID.String()

	var updated Expense
	do(t, srv, "PATCH", path, `{"category":"health","spent_at":"2026-01-02","comment":"аптека"}`, http.StatusOK, &updated)
	if updated.Category != CategoryCodeHealth || updated.SpentAt.String() != "2026-01-02" ||
		updated.Comment == nil || *updated.Comment != "аптека" || updated.Amount != 100 {
		t.Fatalf("updated = %+v", updated)
	}
	do(t, srv, "PATCH", path, `{"comment":""}`, http.StatusOK, &updated)
	if updated.Comment != nil {
		t.Errorf("empty comment must clear it: %+v", updated.Comment)
	}

	tests := []struct{ name, body, field string }{
		{"null spent_at", `{"spent_at":null}`, "spent_at"},
		{"bad spent_at", `{"spent_at":"02.01.2026"}`, "spent_at"},
		{"numeric category", `{"category":5}`, "category"},
		{"unknown category", `{"category":"beer"}`, "category"},
		{"comment not a string", `{"comment":42}`, "comment"},
		{"string amount", `{"amount":"450"}`, "amount"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var resp Error
			do(t, srv, "PATCH", path, tt.body, http.StatusBadRequest, &resp)
			if deref(resp.Error.Field) != tt.field {
				t.Errorf("error = %+v, want field %q", resp.Error, tt.field)
			}
		})
	}

	do(t, srv, "PATCH", "/api/v1/expenses/not-a-uuid", `{"amount":1}`, http.StatusNotFound, nil)
	do(t, srv, "PATCH", "/api/v1/expenses/"+uuid.NewString(), `{"amount":1}`, http.StatusNotFound, nil)
	do(t, srv, "DELETE", "/api/v1/expenses/not-a-uuid", "", http.StatusNotFound, nil)
}

func TestListQueryValidation(t *testing.T) {
	srv := newServer(t)
	for _, q := range []string{"user_id=abc", "from=01.01.2026", "to=yesterday", "offset=-1", "limit=many"} {
		t.Run(q, func(t *testing.T) {
			var resp Error
			do(t, srv, "GET", "/api/v1/expenses?"+q, "", http.StatusBadRequest, &resp)
			if resp.Error.Code != ErrorCodeValidationError {
				t.Errorf("error = %+v", resp.Error)
			}
		})
	}
}

func TestCreateUserOptionalFields(t *testing.T) {
	srv := newServer(t)
	var created User
	do(t, srv, "POST", "/api/v1/users", `{"telegram_id":444,"first_name":"Иван","last_name":"Петров"}`, http.StatusCreated, &created)
	if created.DisplayName != "Иван Петров" || created.LastName == nil || created.Username != nil {
		t.Errorf("created = %+v", created)
	}
	do(t, srv, "POST", "/api/v1/users", `{"telegram_id":0,"first_name":"Иван"}`, http.StatusBadRequest, nil)
	do(t, srv, "POST", "/api/v1/users", `{"telegram_id":1,"first_name":"Иван","age":30}`, http.StatusBadRequest, nil)
	do(t, srv, "POST", "/api/v1/users", ``, http.StatusBadRequest, nil)
}
