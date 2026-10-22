//go:build eval

// Eval-набор: прогон сценариев из ТЗ против реальной LLM.
// Запуск: make eval (LLM_BASE_URL, LLM_API_KEY, LLM_MODEL берутся из окружения).
package agent

import (
	"cmp"
	"context"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"finance-ai-bot/internal/adapter/llm/openai"
	"finance-ai-bot/internal/domain"
	"finance-ai-bot/internal/testutil/memstore"
	"finance-ai-bot/internal/usecase/expense"
	"finance-ai-bot/internal/usecase/user"
)

type evalTurn struct {
	say   string
	check func(t *testing.T, reply string, all []domain.Expense, today time.Time)
}

// evalEnv — агент на реальной LLM поверх хранилища в памяти.
type evalEnv struct {
	agent    *Agent
	expenses *expense.Service
}

func newEvalEnv(t *testing.T) evalEnv {
	t.Helper()
	model := os.Getenv("LLM_MODEL")
	if model == "" {
		t.Skip("LLM_MODEL is not set")
	}
	var temperature *float64
	if v, err := strconv.ParseFloat(os.Getenv("LLM_TEMPERATURE"), 64); err == nil {
		temperature = &v
	}
	client := openai.New(openai.Config{
		BaseURL:     os.Getenv("LLM_BASE_URL"),
		APIKey:      os.Getenv("LLM_API_KEY"),
		Model:       model,
		Temperature: temperature,
		Timeout:     90 * time.Second,
	})

	loc, err := time.LoadLocation(cmp.Or(os.Getenv("APP_TIMEZONE"), "Europe/Moscow"))
	if err != nil {
		t.Fatal(err)
	}

	store := memstore.New()
	ctx := context.Background()
	users := user.NewService(store.Users())
	if _, err := users.Upsert(ctx, domain.User{TelegramID: alice, FirstName: "Алиса"}); err != nil {
		t.Fatal(err)
	}
	if _, err := users.Upsert(ctx, domain.User{TelegramID: bob, FirstName: "Боб"}); err != nil {
		t.Fatal(err)
	}
	expenses := expense.NewService(store.Expenses(), loc)
	// Чужая трата: агент Алисы не должен её видеть.
	if _, err := expenses.Create(ctx, expense.UserScope(bob), expense.CreateParams{
		Amount: 999900, Category: domain.CategoryShopping, Comment: "телевизор",
	}); err != nil {
		t.Fatal(err)
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return evalEnv{agent: New(client, expenses, Config{Location: loc, MaxSteps: 8, Prompts: loadPrompts(t)}, log), expenses: expenses}
}

// run проигрывает диалог Алисы и после каждой реплики проверяет её траты.
func (env evalEnv) run(t *testing.T, turns []evalTurn) {
	ctx := context.Background()
	var history []domain.ChatMessage
	for _, turn := range turns {
		t.Run(turn.say, func(t *testing.T) {
			// «Сегодня» фиксируем до запроса, чтобы прогон около полуночи не давал ложных падений.
			today := env.expenses.Today()
			reply, err := env.agent.Reply(ctx, alice, history, turn.say)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("> %s\n< %s", turn.say, reply)
			history = append(history,
				domain.ChatMessage{Role: domain.ChatRoleUser, Content: turn.say},
				domain.ChatMessage{Role: domain.ChatRoleAssistant, Content: reply})

			res, err := env.expenses.List(ctx, expense.UserScope(alice), domain.ExpenseFilter{Limit: 100})
			if err != nil {
				t.Fatal(err)
			}
			turn.check(t, reply, res.Items, today)
		})
	}
}

func find(all []domain.Expense, comment string) *domain.Expense {
	for i := range all {
		if strings.Contains(all[i].Comment, comment) {
			return &all[i]
		}
	}
	return nil
}

func contains(t *testing.T, reply string, variants ...string) {
	t.Helper()
	norm := strings.ReplaceAll(reply, " ", " ")
	for _, v := range variants {
		if strings.Contains(norm, v) {
			return
		}
	}
	t.Errorf("reply %q contains none of %q", reply, variants)
}

func TestEval(t *testing.T) {
	newEvalEnv(t).run(t, []evalTurn{
		{"кофе 350", func(t *testing.T, _ string, all []domain.Expense, today time.Time) {
			e := find(all, "кофе")
			if e == nil || e.Amount != 35000 || e.Category != domain.CategoryFood || !e.SpentAt.Equal(today) {
				t.Errorf("coffee = %+v", e)
			}
		}},
		{"такси 1200 вчера", func(t *testing.T, _ string, all []domain.Expense, today time.Time) {
			e := find(all, "такси")
			if e == nil || e.Amount != 120000 || e.Category != domain.CategoryTransport || !e.SpentAt.Equal(today.AddDate(0, 0, -1)) {
				t.Errorf("taxi = %+v", e)
			}
		}},
		// Последней добавлена поездка на такси, но исправить нужно трату на 350 — кофе.
		{"последнее было не 350, а 450", func(t *testing.T, _ string, all []domain.Expense, _ time.Time) {
			if e := find(all, "кофе"); e == nil || e.Amount != 45000 || e.Comment != "кофе" {
				t.Errorf("coffee after fix = %+v", e)
			}
			if e := find(all, "такси"); e == nil || e.Amount != 120000 {
				t.Errorf("taxi must stay unchanged: %+v", e)
			}
		}},
		{"аптека 780", func(t *testing.T, _ string, all []domain.Expense, _ time.Time) {
			if e := find(all, "аптек"); e == nil || e.Category != domain.CategoryHealth {
				t.Errorf("pharmacy = %+v", e)
			}
		}},
		{"сколько потратил на еду в этом месяце", func(t *testing.T, reply string, _ []domain.Expense, _ time.Time) {
			contains(t, reply, "450")
		}},
		{"покажи траты за неделю", func(t *testing.T, reply string, _ []domain.Expense, _ time.Time) {
			contains(t, reply, "1200", "1 200")
			if strings.Contains(reply, "телевизор") {
				t.Error("foreign expense leaked")
			}
		}},
		{"удали такси", func(t *testing.T, _ string, all []domain.Expense, _ time.Time) {
			if find(all, "такси") != nil {
				t.Error("taxi was not deleted")
			}
			if len(all) != 2 {
				t.Errorf("expenses left = %d, want 2", len(all))
			}
		}},
	})
}

// Две подходящие траты: агент должен переспросить, а после ответа удалить нужную.
func TestEvalDeleteAmbiguous(t *testing.T) {
	newEvalEnv(t).run(t, []evalTurn{
		{"такси 1200 вчера", func(t *testing.T, _ string, all []domain.Expense, _ time.Time) {
			if len(all) != 1 {
				t.Errorf("expenses = %d, want 1", len(all))
			}
		}},
		{"такси до работы 800", func(t *testing.T, _ string, all []domain.Expense, _ time.Time) {
			if len(all) != 2 {
				t.Errorf("expenses = %d, want 2", len(all))
			}
		}},
		{"удали такси", func(t *testing.T, reply string, all []domain.Expense, _ time.Time) {
			if len(all) != 2 {
				t.Fatalf("deleted without asking: %d left", len(all))
			}
			contains(t, reply, "800")
			contains(t, reply, "1200", "1 200")
		}},
		{"то, что за 800", func(t *testing.T, _ string, all []domain.Expense, _ time.Time) {
			if len(all) != 1 || all[0].Amount != 120000 {
				t.Errorf("want only the 1 200 ₽ taxi left, got %+v", all)
			}
		}},
	})
}
