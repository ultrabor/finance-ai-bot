# CLAUDE.md

Family expense tracker: Telegram bot with an LLM agent + web panel + REST API. Go backend, React frontend, PostgreSQL. Requirements: `docs/test-task-expenses.md`, design: `docs/PROJECT_PASSPORT.md`.

## Commands

- `make test` — Go tests with race detector (no DB/LLM needed; uses `internal/storage/memory`)
- `make test-integration` — SQL repository tests on a throwaway Postgres container (`-tags=integration`, needs Docker)
- `make eval` — agent scenarios against a real LLM (`-tags=eval`, needs `LLM_*` env)
- `go vet ./... && go vet -tags=eval ./... && go vet -tags=integration ./...`
- `cd web && npm run build` — frontend typecheck + build
- `docker compose up --build` — full stack (web :3000, api :8080)

## Rules

- Money is `domain.Money` (kopecks, int64). Never float in storage/logic; float only in JSON output.
- Expense dates are `DATE` / midnight-UTC `time.Time`; "today" comes from `APP_TIMEZONE` via `domain.DateOf`.
- Every service call takes a `service.Scope`. Bot code uses `UserScope(telegramID)`, web API uses `FamilyScope()`. Never let the LLM supply `user_id`.
- Agent tool errors go back to the model as `{"error": "..."}`; only unexpected errors are logged.
- Mutating agent tools (`internal/agent/tools`) must call `turn.Journal.Record` so the user learns about applied changes even if the LLM fails afterwards.
- Bot handles only private chats; `FAMILY_TELEGRAM_IDS` is required unless `TELEGRAM_ALLOW_ALL=true`.
- `memory` store must mirror postgres semantics (ordering, filters) — tests rely on it.
- REST contract lives in `api/openapi.yaml` — update it together with `internal/httpapi` and `web/src/api/types.ts`.
- New migrations: `migrations/0000N_*.sql` (goose, embedded via `migrations/embed.go`).
- User-facing text (bot replies, API error messages, UI) is Russian; code comments are Russian, concise.
