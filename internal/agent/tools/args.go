package tools

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"finance-ai-bot/internal/domain"
)

// flexNumber принимает число или строку с числом: модели иногда присылают "350" или "1 200,50".
type flexNumber float64

func (n *flexNumber) UnmarshalJSON(b []byte) error {
	var f float64
	if err := json.Unmarshal(b, &f); err == nil {
		*n = flexNumber(f)
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return errors.New("ожидается число")
	}
	clean := strings.NewReplacer(" ", "", " ", "", ",", ".", "₽", "").Replace(s)
	if clean == "" {
		*n = 0 // пустая строка — «не передано»
		return nil
	}
	f, err := strconv.ParseFloat(clean, 64)
	if err != nil {
		return fmt.Errorf("ожидается число, получено %q", s)
	}
	*n = flexNumber(f)
	return nil
}

// periodArgs — общий фильтр по периоду и категории для list_expenses и sum_expenses.
type periodArgs struct {
	From     string `json:"from"`
	To       string `json:"to"`
	Category string `json:"category"`
}

func (p periodArgs) filter() (domain.ExpenseFilter, error) {
	var f domain.ExpenseFilter
	if p.From != "" {
		d, err := parseDateArg("from", p.From)
		if err != nil {
			return f, err
		}
		f.From = &d
	}
	if p.To != "" {
		d, err := parseDateArg("to", p.To)
		if err != nil {
			return f, err
		}
		f.To = &d
	}
	if p.Category != "" {
		c := domain.Category(p.Category)
		f.Category = &c
	}
	return f, nil
}

func decodeArgs(raw []byte, dst any) error {
	if len(raw) == 0 {
		raw = []byte("{}")
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return toolError{"некорректные аргументы: " + err.Error()}
	}
	return nil
}

func parseAmount(n flexNumber) (domain.Money, error) {
	m, err := domain.MoneyFromFloat(float64(n))
	if err != nil {
		return 0, toolError{"некорректная сумма"}
	}
	return m, nil
}

func parseDateArg(field, v string) (time.Time, error) {
	d, err := domain.ParseDate(v)
	if err != nil {
		return time.Time{}, toolError{field + ": ожидается дата YYYY-MM-DD"}
	}
	return d, nil
}

func parseID(v string) (uuid.UUID, error) {
	id, err := uuid.Parse(v)
	if err != nil {
		return uuid.Nil, toolError{"некорректный id — возьми id из " + ListExpenses}
	}
	return id, nil
}
