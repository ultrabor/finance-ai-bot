package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"finance-ai-bot/internal/adapter/storage/memory"
	"finance-ai-bot/internal/domain"
	"finance-ai-bot/internal/usecase/expense"
	"finance-ai-bot/internal/usecase/user"
)

type okPinger struct{}

func (okPinger) Ping(context.Context) error { return nil }

func newServer(t *testing.T) *httptest.Server {
	t.Helper()
	store := memory.New()
	users := user.NewService(store.Users())
	for _, id := range []int64{111, 222} {
		if _, err := users.Upsert(context.Background(), domain.User{TelegramID: id, FirstName: "u"}); err != nil {
			t.Fatal(err)
		}
	}
	h := New(expense.NewService(store.Expenses(), time.UTC), users, okPinger{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv := httptest.NewServer(h.Routes())
	t.Cleanup(srv.Close)
	return srv
}

func do(t *testing.T, srv *httptest.Server, method, path, body string, wantStatus int, out any) {
	t.Helper()
	req, err := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != wantStatus {
		t.Fatalf("%s %s: status %d, want %d; body: %s", method, path, resp.StatusCode, wantStatus, data)
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			t.Fatalf("decode %s: %v", data, err)
		}
	}
}

func TestExpenseCRUD(t *testing.T) {
	srv := newServer(t)
	today := time.Now().UTC().Format(domain.DateLayout)

	var created expenseDTO
	do(t, srv, "POST", "/api/v1/expenses",
		`{"user_id":111,"amount":350.5,"category":"food","spent_at":"`+today+`","comment":"кофе"}`,
		http.StatusCreated, &created)
	if created.Amount != 350.5 || created.Comment == nil || *created.Comment != "кофе" {
		t.Fatalf("created = %+v", created)
	}
	do(t, srv, "POST", "/api/v1/expenses",
		`{"user_id":222,"amount":1200,"category":"transport","spent_at":"`+today+`"}`,
		http.StatusCreated, nil)

	var list expenseListDTO
	do(t, srv, "GET", "/api/v1/expenses?user_id=111", "", http.StatusOK, &list)
	if list.Total != 1 || list.Sum != 350.5 || len(list.Items) != 1 {
		t.Fatalf("filtered list = %+v", list)
	}
	do(t, srv, "GET", "/api/v1/expenses?category=transport&from="+today+"&to="+today, "", http.StatusOK, &list)
	if list.Total != 1 || list.Items[0].UserID != 222 {
		t.Fatalf("category list = %+v", list)
	}

	var updated expenseDTO
	do(t, srv, "PATCH", "/api/v1/expenses/"+created.ID, `{"amount":450,"comment":null}`, http.StatusOK, &updated)
	if updated.Amount != 450 || updated.Comment != nil || updated.Category != "food" {
		t.Fatalf("updated = %+v", updated)
	}

	do(t, srv, "DELETE", "/api/v1/expenses/"+created.ID, "", http.StatusNoContent, nil)
	do(t, srv, "GET", "/api/v1/expenses/"+created.ID, "", http.StatusNotFound, nil)
	do(t, srv, "DELETE", "/api/v1/expenses/"+created.ID, "", http.StatusNotFound, nil)
}

func TestValidationErrors(t *testing.T) {
	srv := newServer(t)
	tests := []struct {
		name, body, field string
	}{
		{"zero amount", `{"user_id":111,"amount":0,"category":"food","spent_at":"2026-01-01"}`, "amount"},
		{"three decimals", `{"user_id":111,"amount":1.234,"category":"food","spent_at":"2026-01-01"}`, "amount"},
		{"bad category", `{"user_id":111,"amount":1,"category":"beer","spent_at":"2026-01-01"}`, "category"},
		{"bad date", `{"user_id":111,"amount":1,"category":"food","spent_at":"01.01.2026"}`, "spent_at"},
		{"unknown user", `{"user_id":999,"amount":1,"category":"food","spent_at":"2026-01-01"}`, "user_id"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var resp errorResponse
			do(t, srv, "POST", "/api/v1/expenses", tt.body, http.StatusBadRequest, &resp)
			if resp.Error.Code != "validation_error" || resp.Error.Field != tt.field {
				t.Errorf("error = %+v, want field %q", resp.Error, tt.field)
			}
		})
	}

	var resp errorResponse
	do(t, srv, "POST", "/api/v1/expenses", `{"user_id":`, http.StatusBadRequest, &resp)
	if resp.Error.Code != "bad_request" {
		t.Errorf("malformed JSON: %+v", resp.Error)
	}
	do(t, srv, "GET", "/api/v1/expenses?from=2026-02-01&to=2026-01-01", "", http.StatusBadRequest, nil)
}

func TestUsersAndCategories(t *testing.T) {
	srv := newServer(t)

	var created userDTO
	do(t, srv, "POST", "/api/v1/users", `{"telegram_id":333,"first_name":"Мама","username":"@mama"}`, http.StatusCreated, &created)
	if created.DisplayName != "Мама" || created.Username == nil || *created.Username != "mama" {
		t.Fatalf("created = %+v", created)
	}

	var users itemsResponse[userDTO]
	do(t, srv, "GET", "/api/v1/users", "", http.StatusOK, &users)
	if len(users.Items) != 3 {
		t.Errorf("users = %d, want 3", len(users.Items))
	}

	var cats itemsResponse[categoryDTO]
	do(t, srv, "GET", "/api/v1/categories", "", http.StatusOK, &cats)
	if len(cats.Items) != len(domain.Categories) || cats.Items[0].Label != "еда" {
		t.Errorf("categories = %+v", cats.Items)
	}
}

func TestStrictDecoding(t *testing.T) {
	srv := newServer(t)
	var created expenseDTO
	do(t, srv, "POST", "/api/v1/expenses",
		`{"user_id":111,"amount":100,"category":"food","spent_at":"2026-01-01","comment":"обед"}`,
		http.StatusCreated, &created)
	path := "/api/v1/expenses/" + created.ID

	tests := []struct {
		name, method, path, body string
		status                   int
	}{
		{"string amount", "POST", "/api/v1/expenses", `{"user_id":111,"amount":"350","category":"food","spent_at":"2026-01-01"}`, http.StatusBadRequest},
		{"negative amount", "POST", "/api/v1/expenses", `{"user_id":111,"amount":-5,"category":"food","spent_at":"2026-01-01"}`, http.StatusBadRequest},
		{"trailing garbage", "PATCH", path, `{"amount":5} мусор`, http.StatusBadRequest},
		{"null category", "PATCH", path, `{"category":null}`, http.StatusBadRequest},
		{"null amount", "PATCH", path, `{"amount":null}`, http.StatusBadRequest},
		{"empty patch", "PATCH", path, `{}`, http.StatusBadRequest},
		{"too large", "POST", "/api/v1/expenses", `{"comment":"` + strings.Repeat("я", maxBodySize) + `"}`, http.StatusRequestEntityTooLarge},
		{"limit too big", "GET", "/api/v1/expenses?limit=501", "", http.StatusBadRequest},
		{"limit zero", "GET", "/api/v1/expenses?limit=0", "", http.StatusBadRequest},
		{"wrong method", "PUT", "/api/v1/expenses", `{}`, http.StatusMethodNotAllowed},
		{"unknown route", "GET", "/api/v1/nope", "", http.StatusNotFound},
		{"unknown route POST", "POST", "/api/v1/nope", `{}`, http.StatusNotFound},
		{"too large amount", "POST", "/api/v1/expenses", `{"user_id":111,"amount":100000000.01,"category":"food","spent_at":"2026-01-01"}`, http.StatusBadRequest},
		{"blank user name", "POST", "/api/v1/users", `{"telegram_id":5,"first_name":"   "}`, http.StatusBadRequest},
		{"health", "GET", "/healthz", "", http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			do(t, srv, tt.method, tt.path, tt.body, tt.status, nil)
		})
	}

	var notAllowed errorResponse
	do(t, srv, "DELETE", "/api/v1/expenses", "", http.StatusMethodNotAllowed, &notAllowed)
	if notAllowed.Error.Code != "method_not_allowed" {
		t.Errorf("405 body = %+v", notAllowed)
	}
	// Allow как у самого mux: GET подразумевает HEAD.
	req, _ := http.NewRequest("PUT", srv.URL+"/api/v1/expenses/abc", nil)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if got := resp.Header.Get("Allow"); got != "GET, HEAD, PATCH, DELETE" {
		t.Errorf("Allow = %q", got)
	}
	var tooLarge errorResponse
	do(t, srv, "POST", "/api/v1/expenses", `{"user_id":111,"amount":1000000000,"category":"food","spent_at":"2026-01-01"}`, http.StatusBadRequest, &tooLarge)
	if !strings.Contains(tooLarge.Error.Message, "слишком большая") {
		t.Errorf("too large message = %q", tooLarge.Error.Message)
	}

	var got expenseDTO
	do(t, srv, "GET", path, "", http.StatusOK, &got)
	if got.Amount != 100 || got.Comment == nil || *got.Comment != "обед" {
		t.Errorf("expense changed by rejected requests: %+v", got)
	}
}
