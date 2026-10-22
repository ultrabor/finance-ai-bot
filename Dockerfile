FROM golang:1.26-alpine AS builder

WORKDIR /app

RUN apk --no-cache add ca-certificates

COPY go.mod go.sum ./

# Используем кеш-маунт для папки модулей Go
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .

# Миграции и tzdata встроены в бинарники, отдельные файлы в образе не нужны.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/api ./cmd/api && \
    CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/bot ./cmd/bot

FROM scratch

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /out/api /out/bot /
# Промпт агента читается с диска при старте (AGENT_PROMPT_PATH, по умолчанию prompts/system.md
# от рабочего каталога). В образе лежит копия; docker compose монтирует поверх ./prompts.
WORKDIR /app
COPY prompts /app/prompts

USER 65534:65534

EXPOSE 8080

# Образ общий для api и bot; bot запускается с entrypoint ["/bot"].
ENTRYPOINT ["/api"]
