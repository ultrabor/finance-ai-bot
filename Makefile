# Значения берутся из .env, но переменные окружения важнее:
# POSTGRES_PORT=5433 make test-integration. Так же работает make test-integration POSTGRES_PORT=5433.
# Make по умолчанию даёт .env приоритет над окружением, поэтому значения из окружения
# сохраняются до include и восстанавливаются после.
ENV_KEYS := $(shell sed -n 's/^[[:space:]]*\([A-Za-z_][A-Za-z0-9_]*\)[[:space:]]*=.*/\1/p' .env 2>/dev/null)
ENV_KEEP := $(foreach v,$(ENV_KEYS),$(if $(filter environment,$(origin $(v))),$(v)))
$(foreach v,$(ENV_KEEP),$(eval _env_$(v) := $$(value $(v))))
-include .env
$(foreach v,$(ENV_KEEP),$(eval $(v) := $$(_env_$(v))))
export

CURRENT_DIR=$(shell pwd)

MIGRATION_DIR=${CURRENT_DIR}/migrations
DB_URL="user=${POSTGRES_USER} dbname=${POSTGRES_DB} password=${POSTGRES_PASSWORD} host=${POSTGRES_HOST} port=${POSTGRES_PORT} sslmode=disable"

run:
	go run ./cmd/api

run-bot:
	go run ./cmd/bot

web-dev:
	cd web && npm install && npm run dev

build:
	CGO_ENABLED=0 GOOS=linux go build -o ${CURRENT_DIR}/bin/api ./cmd/api
	CGO_ENABLED=0 GOOS=linux go build -o ${CURRENT_DIR}/bin/bot ./cmd/bot

test:
	go test -race ./...

# Прогон eval-набора фраз против реальной LLM (нужны LLM_* в .env)
eval:
	go test -tags=eval -run TestEval -v -count=1 ./internal/agent/

fmt:
	go fmt ./...

vet:
	go vet ./...

up:
	docker compose up --build

# Postgres с портом на хосте — для make run, make run-bot и make test-integration
db:
	docker compose -f docker-compose.yml -f compose.dev.yml up -d db

# Интеграционные тесты SQL-репозиториев (нужна БД: make db)
test-integration:
	go test -tags=integration -count=1 ./internal/storage/postgres/

down:
	docker compose down

migrate-up:
	goose -dir ${MIGRATION_DIR} postgres ${DB_URL} up

migrate-down:
	goose -dir ${MIGRATION_DIR} postgres ${DB_URL} down

migrate-status:
	goose -dir ${MIGRATION_DIR} postgres ${DB_URL} status
