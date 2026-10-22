// Package prompts — тексты для модели. Весь текст лежит в system.tmpl (text/template):
// системный промпт и вспомогательные шаблоны ({{define}}); здесь только данные для подстановки.
package prompts

import (
	_ "embed"
	"fmt"
	"os"
	"strings"
	"text/template"
	"time"

	"finance-ai-bot/internal/domain"
)

//go:embed system.tmpl
var systemTmpl string

// nudgeTemplate — подсказка модели, заявившей об изменении трат без вызова инструмента.
const nudgeTemplate = "unconfirmed_nudge"

var weekdaysRU = [...]string{"воскресенье", "понедельник", "вторник", "среда", "четверг", "пятница", "суббота"}

// Prompts — разобранный шаблон промптов.
type Prompts struct {
	tmpl *template.Template
}

// Embedded — шаблон, встроенный в бинарник.
func Embedded() *Prompts {
	p, err := parse(systemTmpl)
	if err != nil {
		panic(err) // встроенный шаблон проверяют тесты
	}
	return p
}

// Load читает шаблон из файла и проверяет его пробным рендером, чтобы ошибка
// (опечатка в поле, неизвестная категория) остановила запуск, а не первый ответ.
func Load(path string) (*Prompts, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read prompt template: %w", err)
	}
	p, err := parse(string(data))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if _, err := p.System(NewCalendar(time.Now(), time.UTC)); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if _, err := p.UnconfirmedNudge(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return p, nil
}

func parse(text string) (*Prompts, error) {
	t, err := template.New("system").Option("missingkey=error").Funcs(template.FuncMap{"label": label}).Parse(text)
	if err != nil {
		return nil, fmt.Errorf("parse prompt template: %w", err)
	}
	if t.Lookup(nudgeTemplate) == nil {
		return nil, fmt.Errorf("prompt template: missing {{define %q}}", nudgeTemplate)
	}
	return &Prompts{tmpl: t}, nil
}

// label — русское название категории по коду; неизвестный код — ошибка рендера,
// чтобы опечатка в шаблоне не ушла в промпт.
func label(code string) (string, error) {
	c := domain.Category(code)
	if !c.Valid() {
		return "", fmt.Errorf("unknown category %q", code)
	}
	return c.Label(), nil
}

// Period — интервал дат YYYY-MM-DD, обе границы включительно.
type Period struct {
	From, To string
}

// Calendar — данные системного промпта: опорные даты в часовом поясе семьи.
type Calendar struct {
	TZ                            string
	Today, Weekday                string
	Yesterday, DayBefore          string
	Last7Days, ThisWeek, LastWeek Period
	ThisMonth, LastMonth          Period
}

// NewCalendar считает опорные даты для момента now в часовом поясе loc.
func NewCalendar(now time.Time, loc *time.Location) Calendar {
	today := domain.DateOf(now, loc)
	weekStart := today.AddDate(0, 0, -((int(today.Weekday()) + 6) % 7))
	monthStart := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, time.UTC)

	return Calendar{
		TZ:        loc.String(),
		Today:     day(today),
		Weekday:   weekdaysRU[today.Weekday()],
		Yesterday: day(today.AddDate(0, 0, -1)),
		DayBefore: day(today.AddDate(0, 0, -2)),
		Last7Days: period(today.AddDate(0, 0, -6), today),
		ThisWeek:  period(weekStart, weekStart.AddDate(0, 0, 6)),
		LastWeek:  period(weekStart.AddDate(0, 0, -7), weekStart.AddDate(0, 0, -1)),
		ThisMonth: period(monthStart, monthStart.AddDate(0, 1, -1)),
		LastMonth: period(monthStart.AddDate(0, -1, 0), monthStart.AddDate(0, 0, -1)),
	}
}

// System рендерит системный промпт.
func (p *Prompts) System(c Calendar) (string, error) {
	return p.render("", c)
}

// UnconfirmedNudge — системное сообщение модели, которая ответила «Записал…», не вызвав инструмент.
func (p *Prompts) UnconfirmedNudge() (string, error) {
	return p.render(nudgeTemplate, nil)
}

func (p *Prompts) render(name string, data any) (string, error) {
	t := p.tmpl
	if name != "" {
		t = p.tmpl.Lookup(name)
	}
	var b strings.Builder
	if err := t.Execute(&b, data); err != nil {
		return "", fmt.Errorf("render prompt %q: %w", t.Name(), err)
	}
	return strings.TrimSpace(b.String()), nil
}

func period(from, to time.Time) Period { return Period{From: day(from), To: day(to)} }

func day(t time.Time) string { return t.Format(domain.DateLayout) }
