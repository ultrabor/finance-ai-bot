// Package prompts — системный промпт агента. Текст лежит в system.tmpl (text/template),
// здесь только данные для подстановки: календарь и категории.
package prompts

import (
	_ "embed"
	"fmt"
	"strings"
	"text/template"
	"time"

	"finance-ai-bot/internal/domain"
)

//go:embed system.tmpl
var systemTmpl string

var tmpl = template.Must(template.New("system").Option("missingkey=error").Parse(systemTmpl))

var weekdaysRU = [...]string{"воскресенье", "понедельник", "вторник", "среда", "четверг", "пятница", "суббота"}

// categoryHints подсказывают модели, какие траты относятся к категории.
var categoryHints = map[domain.Category]string{
	domain.CategoryFood:          "продукты, кафе, рестораны, кофе, обеды, доставка еды",
	domain.CategoryTransport:     "такси, метро, автобус, электричка, бензин, парковка, каршеринг",
	domain.CategoryHousing:       "аренда, ипотека, коммунальные услуги, интернет, связь, ремонт",
	domain.CategoryEntertainment: "кино, концерты, театр, игры, подписки, бар, хобби",
	domain.CategoryHealth:        "аптека, лекарства, врач, анализы, стоматология, спортзал",
	domain.CategoryShopping:      "одежда, обувь, техника, бытовые товары, подарки",
	domain.CategoryOther:         "всё, что не подходит к остальным",
}

// Period — интервал дат YYYY-MM-DD, обе границы включительно.
type Period struct {
	From, To string
}

type CategoryHint struct {
	Code, Label, Hint string
}

// Calendar — данные шаблона: опорные даты в часовом поясе семьи и категории.
type Calendar struct {
	TZ                            string
	Today, Weekday                string
	Yesterday, DayBefore          string
	Last7Days, ThisWeek, LastWeek Period
	ThisMonth, LastMonth          Period
	Categories                    []CategoryHint
}

// NewCalendar считает опорные даты для момента now в часовом поясе loc.
func NewCalendar(now time.Time, loc *time.Location) Calendar {
	today := domain.DateOf(now, loc)
	weekStart := today.AddDate(0, 0, -((int(today.Weekday()) + 6) % 7))
	monthStart := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, time.UTC)

	cats := make([]CategoryHint, len(domain.Categories))
	for i, c := range domain.Categories {
		cats[i] = CategoryHint{Code: string(c), Label: c.Label(), Hint: categoryHints[c]}
	}
	return Calendar{
		TZ:         loc.String(),
		Today:      day(today),
		Weekday:    weekdaysRU[today.Weekday()],
		Yesterday:  day(today.AddDate(0, 0, -1)),
		DayBefore:  day(today.AddDate(0, 0, -2)),
		Last7Days:  period(today.AddDate(0, 0, -6), today),
		ThisWeek:   period(weekStart, weekStart.AddDate(0, 0, 6)),
		LastWeek:   period(weekStart.AddDate(0, 0, -7), weekStart.AddDate(0, 0, -1)),
		ThisMonth:  period(monthStart, monthStart.AddDate(0, 1, -1)),
		LastMonth:  period(monthStart.AddDate(0, -1, 0), monthStart.AddDate(0, 0, -1)),
		Categories: cats,
	}
}

// System рендерит системный промпт.
func System(c Calendar) (string, error) {
	var b strings.Builder
	if err := tmpl.Execute(&b, c); err != nil {
		return "", fmt.Errorf("render system prompt: %w", err)
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

func period(from, to time.Time) Period { return Period{From: day(from), To: day(to)} }

func day(t time.Time) string { return t.Format(domain.DateLayout) }
