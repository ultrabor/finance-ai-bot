# Значения берутся из .env, но переменные окружения важнее:
# POSTGRES_PORT=5433 make run. Так же работает make run POSTGRES_PORT=5433.
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

# Postgres с портом на хосте — для make run и make run-bot
db:
	docker compose -f docker-compose.yml -f compose.dev.yml up -d db

# Интеграционные тесты SQL-репозиториев на одноразовом Postgres (БД приложения не трогается).
# Нужен только Docker; порт меняется через ITEST_PORT.
ITEST_PORT ?= 55432
ITEST_NAME := finance-ai-bot-itest
test-integration:
	@docker rm -f $(ITEST_NAME) >/dev/null 2>&1 || true
	docker run --rm -d --name $(ITEST_NAME) -e POSTGRES_USER=test -e POSTGRES_PASSWORD=test \
		-e POSTGRES_DB=test -p 127.0.0.1:$(ITEST_PORT):5432 postgres:16-alpine >/dev/null
	@trap 'docker stop $(ITEST_NAME) >/dev/null' EXIT; \
	for i in $$(seq 30); do docker exec $(ITEST_NAME) pg_isready -h 127.0.0.1 -U test -d test -q && break; sleep 1; done; \
	POSTGRES_USER=test POSTGRES_PASSWORD=test POSTGRES_DB=test POSTGRES_HOST=127.0.0.1 POSTGRES_PORT=$(ITEST_PORT) \
		go test -tags=integration -count=1 ./internal/storage/postgres/

down:
	docker compose down

migrate-up:
	goose -dir ${MIGRATION_DIR} postgres ${DB_URL} up

migrate-down:
	goose -dir ${MIGRATION_DIR} postgres ${DB_URL} down

migrate-status:
	goose -dir ${MIGRATION_DIR} postgres ${DB_URL} status
