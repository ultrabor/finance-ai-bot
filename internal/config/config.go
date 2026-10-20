// Package config загружает настройки приложения из переменных окружения.
package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
)

type Config struct {
	AppEnv   string `env:"APP_ENV" envDefault:"development"`
	LogLevel string `env:"LOG_LEVEL" envDefault:"info"`
	// Timezone — часовой пояс семьи: от него считаются «сегодня», «вчера», «этот месяц».
	Timezone string `env:"APP_TIMEZONE" envDefault:"Europe/Moscow"`

	HTTP     HTTP
	Postgres Postgres
	Telegram Telegram
	LLM      LLM
}

type HTTP struct {
	Host string `env:"HTTP_HOST" envDefault:"0.0.0.0"`
	Port int    `env:"HTTP_PORT" envDefault:"8080"`
}

type Postgres struct {
	User     string `env:"POSTGRES_USER,required"`
	Password string `env:"POSTGRES_PASSWORD,required"`
	Host     string `env:"POSTGRES_HOST" envDefault:"localhost"`
	Port     int    `env:"POSTGRES_PORT" envDefault:"5432"`
	DB       string `env:"POSTGRES_DB,required"`
	SSLMode  string `env:"POSTGRES_SSLMODE" envDefault:"disable"`
	MaxConns int    `env:"POSTGRES_MAX_CONNS" envDefault:"10"`
}

type Telegram struct {
	Token string `env:"TELEGRAM_BOT_TOKEN"`
	// AllowedIDs — белый список членов семьи.
	AllowedIDs IDList `env:"FAMILY_TELEGRAM_IDS"`
	// AllowAll разрешает работу без белого списка (только для демо): иначе любой,
	// кто найдёт бота, расходует бюджет LLM и попадает в панель как член семьи.
	AllowAll bool `env:"TELEGRAM_ALLOW_ALL" envDefault:"false"`
}

type LLM struct {
	BaseURL string        `env:"LLM_BASE_URL" envDefault:"https://api.openai.com/v1"`
	APIKey  string        `env:"LLM_API_KEY"`
	Model   string        `env:"LLM_MODEL" envDefault:"gpt-4.1-mini"`
	Timeout time.Duration `env:"LLM_TIMEOUT" envDefault:"30s"`
	// Temperature не задана — параметр не передаётся (часть моделей его не поддерживает).
	Temperature *float64 `env:"LLM_TEMPERATURE"`
	// MaxSteps — лимит итераций tool calling на одно сообщение.
	MaxSteps int `env:"LLM_MAX_STEPS" envDefault:"8"`
}

// IDList — список Telegram ID через запятую; пробелы и пустые элементы допускаются ("1, 2,").
type IDList []int64

func (l *IDList) UnmarshalText(text []byte) error {
	var out IDList
	for part := range strings.SplitSeq(string(text), ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, err := strconv.ParseInt(part, 10, 64)
		if err != nil || id <= 0 {
			return fmt.Errorf("некорректный Telegram ID %q", part)
		}
		out = append(out, id)
	}
	*l = out
	return nil
}

func Load() (*Config, error) {
	cfg, err := env.ParseAs[Config]()
	if err != nil {
		return nil, fmt.Errorf("parse env: %w", err)
	}
	if _, err := cfg.Location(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// ValidateBot проверяет настройки, обязательные только для Telegram-бота.
func (c *Config) ValidateBot() error {
	var errs []error
	if c.Telegram.Token == "" {
		errs = append(errs, errors.New("TELEGRAM_BOT_TOKEN is required"))
	}
	if len(c.Telegram.AllowedIDs) == 0 && !c.Telegram.AllowAll {
		errs = append(errs, errors.New("FAMILY_TELEGRAM_IDS is required (or set TELEGRAM_ALLOW_ALL=true for a demo)"))
	}
	if c.LLM.BaseURL == "" {
		errs = append(errs, errors.New("LLM_BASE_URL is required"))
	}
	if c.LLM.Model == "" {
		errs = append(errs, errors.New("LLM_MODEL is required"))
	}
	if c.LLM.MaxSteps < 1 {
		errs = append(errs, errors.New("LLM_MAX_STEPS must be positive"))
	}
	return errors.Join(errs...)
}

func (c *Config) Location() (*time.Location, error) {
	loc, err := time.LoadLocation(c.Timezone)
	if err != nil {
		return nil, fmt.Errorf("APP_TIMEZONE: %w", err)
	}
	return loc, nil
}

func (h HTTP) Addr() string {
	return net.JoinHostPort(h.Host, strconv.Itoa(h.Port))
}

// DSN собирает строку подключения pgx.
func (p Postgres) DSN() string {
	u := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(p.User, p.Password),
		Host:   net.JoinHostPort(p.Host, strconv.Itoa(p.Port)),
		Path:   p.DB,
	}
	q := url.Values{}
	q.Set("sslmode", p.SSLMode)
	q.Set("pool_max_conns", strconv.Itoa(p.MaxConns))
	u.RawQuery = q.Encode()
	return u.String()
}
