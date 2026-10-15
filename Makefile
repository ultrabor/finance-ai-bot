-include .env
export

CURRENT_DIR=$(shell pwd)

APP=$(shell basename ${CURRENT_DIR})

APP_CMD_DIR=${CURRENT_DIR}/cmd/api

TAG=latest
ENV_TAG=latest
PROJECT_NAME=bazaar
MIGRATION_DIR=${CURRENT_DIR}/migrations
DB_URL="user=${POSTGRES_USER} dbname=${POSTGRES_DB} password=${POSTGRES_PASSWORD} host=${POSTGRES_HOST} port=${POSTGRES_PORT} sslmode=disable"

run:
	go run ./cmd/api/main.go

build:
	CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o ${CURRENT_DIR}/bin/${APP} ${APP_CMD_DIR}/main.go

test:
	go test ./...

fmt:
	go fmt ./...

vet:
	go vet ./...

up:
	docker compose up 

down:
	docker compose down

migrate-up:
	goose -dir ${MIGRATION_DIR} postgres ${DB_URL} up

migrate-down:
	goose -dir ${MIGRATION_DIR} postgres ${DB_URL} down

migrate-status:
	goose -dir ${MIGRATION_DIR} postgres ${DB_URL} status

swag_init:
	swag init -g cmd/api/main.go -o api/docs --parseDependency --parseInternal