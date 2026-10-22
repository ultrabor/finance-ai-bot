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
	got, err := System(NewCalendar(fixedNow, moscow(t)))
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
	c.Categories = nil
	if !reflect.DeepEqual(c, want) {
		t.Errorf("calendar = %+v\nwant       %+v", c, want)
	}
}

// Каждая категория домена попадает в промпт с подсказкой.
func TestEveryCategoryHasHint(t *testing.T) {
	p, err := System(NewCalendar(fixedNow, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range domain.Categories {
		hint, ok := categoryHints[c]
		if !ok || hint == "" {
			t.Errorf("no hint for category %q", c)
			continue
		}
		if line := "- " + string(c) + " — " + c.Label() + ": " + hint; !strings.Contains(p, line) {
			t.Errorf("prompt missing %q", line)
		}
	}
}
