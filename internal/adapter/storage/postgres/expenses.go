package postgres

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"finance-ai-bot/internal/domain"
)

const expenseColumns = `id, user_id, amount, category, spent_at, COALESCE(comment, ''), created_at, updated_at`

type ExpenseRepo struct {
	pool *pgxpool.Pool
}

func NewExpenseRepo(pool *pgxpool.Pool) *ExpenseRepo {
	return &ExpenseRepo{pool: pool}
}

func (r *ExpenseRepo) Create(ctx context.Context, e domain.Expense) (domain.Expense, error) {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO expenses (user_id, amount, category, spent_at, comment)
		VALUES ($1, $2, $3, $4, NULLIF($5, ''))
		RETURNING `+expenseColumns,
		e.UserID, e.Amount, e.Category, e.SpentAt, e.Comment)
	out, err := scanExpense(row)
	if err != nil {
		return domain.Expense{}, mapError(err)
	}
	return out, nil
}

func (r *ExpenseRepo) Get(ctx context.Context, id uuid.UUID, ownerID *int64) (domain.Expense, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT `+expenseColumns+` FROM expenses
		WHERE id = $1 AND ($2::bigint IS NULL OR user_id = $2)`,
		id, ownerID)
	e, err := scanExpense(row)
	if err != nil {
		return domain.Expense{}, mapError(err)
	}
	return e, nil
}

// querier — общий интерфейс пула и транзакции.
type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

func (r *ExpenseRepo) List(ctx context.Context, f domain.ExpenseFilter) ([]domain.Expense, domain.ExpenseSummary, error) {
	// Страница и итоги читаются из одного снимка, чтобы total/sum совпадали с items.
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, domain.ExpenseSummary{}, fmt.Errorf("begin list tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	items, err := listExpenses(ctx, tx, f)
	if err != nil {
		return nil, domain.ExpenseSummary{}, err
	}
	sum, err := summarize(ctx, tx, f)
	if err != nil {
		return nil, domain.ExpenseSummary{}, err
	}
	return items, sum, tx.Commit(ctx)
}

func (r *ExpenseRepo) Summary(ctx context.Context, f domain.ExpenseFilter) (domain.ExpenseSummary, error) {
	return summarize(ctx, r.pool, f)
}

func listExpenses(ctx context.Context, q querier, f domain.ExpenseFilter) ([]domain.Expense, error) {
	where, args := buildWhere(f)
	order := "spent_at DESC, created_at DESC"
	if f.Sort == domain.SortByCreatedAt {
		order = "created_at DESC"
	}
	args = append(args, f.Limit, f.Offset)
	query := fmt.Sprintf(`SELECT %s FROM expenses %s ORDER BY %s, id LIMIT $%d OFFSET $%d`,
		expenseColumns, where, order, len(args)-1, len(args))

	rows, err := q.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list expenses: %w", err)
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Expense, error) {
		return scanExpense(row)
	})
	if err != nil {
		return nil, fmt.Errorf("list expenses: %w", err)
	}
	return items, nil
}

func summarize(ctx context.Context, q querier, f domain.ExpenseFilter) (domain.ExpenseSummary, error) {
	where, args := buildWhere(f)
	rows, err := q.Query(ctx, `
		SELECT category, SUM(amount), COUNT(*) FROM expenses `+where+`
		GROUP BY category ORDER BY SUM(amount) DESC, category`, args...)
	if err != nil {
		return domain.ExpenseSummary{}, fmt.Errorf("summarize expenses: %w", err)
	}
	totals, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.CategoryTotal, error) {
		var t domain.CategoryTotal
		err := row.Scan(&t.Category, &t.Amount, &t.Count)
		return t, err
	})
	if err != nil {
		return domain.ExpenseSummary{}, fmt.Errorf("summarize expenses: %w", err)
	}

	sum := domain.ExpenseSummary{ByCategory: totals}
	for _, t := range totals {
		sum.Total += t.Amount
		sum.Count += t.Count
	}
	return sum, nil
}

func (r *ExpenseRepo) Update(ctx context.Context, id uuid.UUID, ownerID *int64, p domain.ExpensePatch) (domain.Expense, error) {
	row := r.pool.QueryRow(ctx, `
		UPDATE expenses SET
			amount     = COALESCE($3, amount),
			category   = COALESCE($4, category),
			spent_at   = COALESCE($5, spent_at),
			comment    = CASE WHEN $6::text IS NULL THEN comment ELSE NULLIF($6, '') END,
			updated_at = now()
		WHERE id = $1 AND ($2::bigint IS NULL OR user_id = $2)
		RETURNING `+expenseColumns,
		id, ownerID, p.Amount, p.Category, p.SpentAt, p.Comment)
	e, err := scanExpense(row)
	if err != nil {
		return domain.Expense{}, mapError(err)
	}
	return e, nil
}

func (r *ExpenseRepo) Delete(ctx context.Context, id uuid.UUID, ownerID *int64) (domain.Expense, error) {
	row := r.pool.QueryRow(ctx, `
		DELETE FROM expenses WHERE id = $1 AND ($2::bigint IS NULL OR user_id = $2)
		RETURNING `+expenseColumns,
		id, ownerID)
	e, err := scanExpense(row)
	if err != nil {
		return domain.Expense{}, mapError(err)
	}
	return e, nil
}

func buildWhere(f domain.ExpenseFilter) (string, []any) {
	var (
		conds []string
		args  []any
	)
	add := func(cond string, arg any) {
		args = append(args, arg)
		conds = append(conds, strings.ReplaceAll(cond, "?", "$"+strconv.Itoa(len(args))))
	}
	if f.UserID != nil {
		add("user_id = ?", *f.UserID)
	}
	if f.From != nil {
		add("spent_at >= ?", *f.From)
	}
	if f.To != nil {
		add("spent_at <= ?", *f.To)
	}
	if f.Category != nil {
		add("category = ?", *f.Category)
	}
	if f.Query != "" {
		add("comment ILIKE '%' || ? || '%'", escapeLike(f.Query))
	}
	if len(conds) == 0 {
		return "", args
	}
	return "WHERE " + strings.Join(conds, " AND "), args
}

func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

func scanExpense(row pgx.Row) (domain.Expense, error) {
	var e domain.Expense
	err := row.Scan(&e.ID, &e.UserID, &e.Amount, &e.Category, &e.SpentAt, &e.Comment, &e.CreatedAt, &e.UpdatedAt)
	return e, err
}

func mapError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok {
		switch pgErr.Code {
		case "23503": // foreign_key_violation
			return domain.NewValidationError("user_id", "член семьи не найден")
		case "23514": // check_violation
			return domain.NewValidationError("body", "некорректные данные траты")
		}
	}
	return err
}
