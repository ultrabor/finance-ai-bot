# CLAUDE.md

Family expense tracker: Telegram bot with an LLM agent + web panel + REST API. Go backend, React frontend, PostgreSQL. Requirements: `docs/test-task-expenses.md`, design: `docs/PROJECT_PASSPORT.md`.

## Commands

- `make generate` — regenerate HTTP models from `api/openapi.yaml` (oapi-codegen, needs network on first run)
- `make test` — Go tests with race detector (no DB/LLM needed; uses `internal/testutil/memstore`)
- `make test-integration` — SQL repository tests on a throwaway Postgres container (`-tags=integration`, needs Docker)
- `make eval` — agent scenarios against a real LLM (`-tags=eval`, needs `LLM_*` env)
- `make lint` — golangci-lint incl. depguard layer rules (`.golangci.yml`)
- `go vet ./... && go vet -tags=eval ./... && go vet -tags=integration ./...`
- `cd web && npm run build` — frontend typecheck + build
- `docker compose up --build` — full stack (web :3000, api :8080)

## Layout

- `internal/domain` — entities, `Money`, `Category`, errors. Imports nothing from `internal/`.
- `internal/usecase/{expense,user}` — business rules; repository ports in `ports.go`.
- `internal/agent` — LLM ↔ tools loop and journal; `agent/llm` (LLM port), `agent/tools` (one file per tool; model-facing description + JSON schema in `agent/tools/schemas/<name>.json`), `agent/prompts` (`system.tmpl` + golden test).
- `internal/adapter` — `llm/openai`, `storage/postgres`: implement ports.
- `internal/testutil/memstore` — in-memory store for tests and eval only; depguard forbids it in non-test code.
- `internal/transport/{httpapi,telegram}` — depend on use case interfaces they declare.
- `internal/app` — composition root (`RunAPI`, `RunBot`); `cmd/*` only load config and call it.
- Dependency rule: transport, agent → usecase → domain; adapters implement ports; nothing but `app` imports adapters. Enforced by depguard (`make lint`) — fix the design, don't loosen the rule.

## Rules

- Money is `domain.Money` (kopecks, int64). Never float in storage/logic; float only in JSON output.
- Expense dates are `DATE` / midnight-UTC `time.Time`; "today" comes from `APP_TIMEZONE` via `domain.DateOf`.
- Every expense use case call takes an `expense.Scope`. Bot code uses `UserScope(telegramID)`, web API uses `FamilyScope()`. Never let the LLM supply `user_id`.
- Agent tool errors go back to the model as `{"error": "..."}`; only unexpected errors are logged.
- Mutating agent tools (`internal/agent/tools`) must call `turn.Journal.Record` so the user learns about applied changes even if the LLM fails afterwards.
- Bot handles only private chats; `FAMILY_TELEGRAM_IDS` is required unless `TELEGRAM_ALLOW_ALL=true`.
- `memstore` must mirror postgres semantics (ordering, filters) — tests rely on it.
- REST contract lives in `api/openapi.yaml`. After changing it run `make generate` (Go models → `internal/transport/httpapi/types.gen.go`, never edit by hand) and update the handlers and `web/src/api/types.ts`.
- Every text the model reads lives outside Go code: system prompt, category hints and the retry nudge in `internal/agent/prompts/system.tmpl`, tool descriptions/schemas in `internal/agent/tools/schemas/*.json`. System prompt text lives in `internal/agent/prompts/system.tmpl`; after editing run `go test ./internal/agent/prompts -update` and review `testdata/system.golden`.
- New migrations: `migrations/0000N_*.sql` (goose, embedded via `migrations/embed.go`).
- User-facing text (bot replies, API error messages, UI) is Russian; code comments are Russian, concise.
