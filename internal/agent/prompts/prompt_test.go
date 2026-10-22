package prompts

import (
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
	_ "time/tzdata"

	"finance-ai-bot/internal/domain"
)

// promptFile — промпт из корня репозитория, тот же, что читает бот.
var promptFile = filepath.Join("..", "..", "..", "prompts", "system.md")

func mustLoad(t *testing.T) *Prompts {
	t.Helper()
	p, err := Load(promptFile)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

var update = flag.Bool("update", false, "перезаписать testdata/*.golden")

// 23:30 UTC 15 октября — в Москве уже пятница, 16 октября.
var fixedNow = time.Date(2026, 10, 15, 23, 30, 0, 0, time.UTC)

func moscow(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func TestSystemGolden(t *testing.T) {
	got, err := mustLoad(t).System(NewCalendar(fixedNow, moscow(t)))
	if err != nil {
		t.Fatal(err)
	}
	golden := filepath.Join("testdata", "system.golden")
	if *update {
		if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("prompt differs from %s; run go test ./internal/agent/prompts -update and review the diff\n--- got:\n%s", golden, got)
	}
}

func TestCalendar(t *testing.T) {
	c := NewCalendar(fixedNow, moscow(t))
	want := Calendar{
		TZ: "Europe/Moscow", Today: "2026-10-16", Weekday: "пятница",
		Yesterday: "2026-10-15", DayBefore: "2026-10-14",
		Last7Days: Period{"2026-10-10", "2026-10-16"},
		ThisWeek:  Period{"2026-10-12", "2026-10-18"},
		LastWeek:  Period{"2026-10-05", "2026-10-11"},
		ThisMonth: Period{"2026-10-01", "2026-10-31"},
		LastMonth: Period{"2026-09-01", "2026-09-30"},
	}
	if !reflect.DeepEqual(c, want) {
		t.Errorf("calendar = %+v\nwant       %+v", c, want)
	}
}

// Каждая категория домена попадает в промпт с названием из domain и подсказкой.
func TestEveryCategoryIsDescribed(t *testing.T) {
	p, err := mustLoad(t).System(NewCalendar(fixedNow, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range domain.Categories {
		prefix := "- " + string(c) + " — " + c.Label() + ": "
		i := strings.Index(p, prefix)
		if i < 0 {
			t.Errorf("prompt does not describe category %q", c)
			continue
		}
		if hint, _, _ := strings.Cut(p[i+len(prefix):], "\n"); strings.TrimSpace(hint) == "" {
			t.Errorf("category %q has no hint", c)
		}
	}
}

func TestUnconfirmedNudge(t *testing.T) {
	nudge, err := mustLoad(t).UnconfirmedNudge()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(nudge, "Ты ответил, что изменил траты") || strings.Contains(nudge, "Календарь") {
		t.Errorf("nudge = %q", nudge)
	}
}

func TestParseRejectsBrokenTemplates(t *testing.T) {
	tests := map[string]string{
		"syntax":           "{{.Today",
		"no nudge":         "сегодня {{.Today}}",
		"unknown field":    `{{.Tomorrow}}{{define "unconfirmed_nudge"}}x{{end}}`,
		"unknown category": `{{label "beer"}}{{define "unconfirmed_nudge"}}x{{end}}`,
	}
	for name, text := range tests {
		t.Run(name, func(t *testing.T) {
			p, err := parse(text)
			if err == nil {
				_, err = p.System(NewCalendar(fixedNow, time.UTC))
			}
			if err == nil {
				t.Error("broken template accepted")
			}
		})
	}
}

func TestLoadFromFile(t *testing.T) {
	dir := t.TempDir()
	write := func(name, text string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}

	custom := write("custom.tmpl", `Сегодня {{.Today}}, {{label "food"}}.{{define "unconfirmed_nudge"}}Вызови инструмент.{{end}}`)
	p, err := Load(custom)
	if err != nil {
		t.Fatal(err)
	}
	got, err := p.System(NewCalendar(fixedNow, time.UTC))
	if err != nil || got != "Сегодня 2026-10-15, еда." {
		t.Errorf("System = %q, %v", got, err)
	}
	if nudge, _ := p.UnconfirmedNudge(); nudge != "Вызови инструмент." {
		t.Errorf("nudge = %q", nudge)
	}

	// Ошибки обнаруживаются при загрузке, а не при первом сообщении.
	for name, text := range map[string]string{
		"unknown field.tmpl":    `{{.Tomorrow}}{{define "unconfirmed_nudge"}}x{{end}}`,
		"unknown category.tmpl": `{{label "beer"}}{{define "unconfirmed_nudge"}}x{{end}}`,
		"no nudge.tmpl":         `{{.Today}}`,
	} {
		if _, err := Load(write(name, text)); err == nil {
			t.Errorf("%s: broken template accepted", name)
		}
	}
	if _, err := Load(filepath.Join(dir, "missing.tmpl")); err == nil {
		t.Error("missing file accepted")
	}
}
