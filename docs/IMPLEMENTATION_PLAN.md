# План реализации: Finance AI Bot

Основа — [паспорт проекта](PROJECT_PASSPORT.md). Срок — 5 рабочих дней, последний день — запас и полировка.

## Структура репозитория

```
finance-ai-bot/
├── cmd/
│   ├── api/            # REST API + миграции при старте
│   └── bot/            # Telegram-бот + агент
├── internal/
│   ├── config/         # загрузка env
│   ├── domain/         # Expense, Category, ошибки, фильтры
│   ├── storage/postgres/  # репозитории, миграции (embed)
│   ├── service/        # бизнес-логика, валидация
│   ├── httpapi/        # хендлеры, DTO, middleware
│   ├── llm/            # интерфейс клиента + OpenAI-compatible реализация
│   ├── agent/          # цикл tool calling, инструменты, промпт
│   └── telegram/       # адаптер бота, история диалога
├── web/                # React + Vite
├── api/openapi.yaml
├── deploy/             # Dockerfile, nginx.conf
├── docker-compose.yml
├── .env.example
├── Makefile
├── CLAUDE.md
└── README.md
```

## День 1 — каркас, БД, REST API

1. `git init`, `go mod init`, `.gitignore`, `Makefile` (`run`, `test`, `lint`, `up`), `CLAUDE.md`.
2. `internal/config`: структура конфига, валидация обязательных полей.
3. Миграции goose: `users`, `expenses` (CHECK по категориям, `amount > 0`, индексы), `chat_messages`.
4. `internal/domain`: `Expense`, `Category` (коды + русские названия), `ExpenseFilter`, `ErrNotFound`, `ErrValidation`.
5. `internal/storage/postgres`: `ExpenseRepo` (Create, Get, List с фильтрами + total + sum, Update, Delete — все с опциональным `userID`-скоупом), `UserRepo` (Upsert, List).
6. `internal/service`: валидация, проставление дефолтов, `updated_at`.
7. `internal/httpapi`: роуты из паспорта, единый формат ошибок, логирование запросов, recover.
8. `cmd/api`: сборка зависимостей, миграции, graceful shutdown.
9. Dockerfile (multi-stage) + `docker-compose.yml` с `postgres` и `api`.
10. Тесты: репозиторий на testcontainers, хендлеры через `httptest`.

**Результат:** `docker compose up` → рабочий REST API с curl-проверкой.

## День 2 — LLM и агент

1. `internal/llm`: интерфейс `Client.Chat(ctx, messages, tools) (Response, error)`; реализация на `openai-go` с `base_url`, моделью, таймаутом, ретраем на 429/5xx.
2. `internal/agent`:
   - JSON Schema инструментов (`add_expense`, `list_expenses`, `sum_expenses`, `update_expense`, `delete_expense`); `category` — enum, даты — `YYYY-MM-DD`.
   - Инструменты — замыкания над `service` с подставленным `userID` из Telegram.
   - Цикл: запрос → tool calls → результаты → повтор, максимум 6 итераций.
   - Системный промпт: роль, текущая дата/день недели/TZ, словарь категорий с примерами («кофе» → еда, «аптека» → здоровье), правила: для изменения/удаления сначала искать, при неоднозначности уточнять, отвечать кратко по-русски, суммы с ₽.
3. Unit-тесты агента с фейковым LLM-клиентом (скриптованные tool calls): проверка, что `userID` берётся не из LLM, лимит итераций, обработка ошибок инструмента.

**Результат:** агент вызывается из теста/CLI и выполняет CRUD.

## День 3 — Telegram-бот и качество агента

1. `internal/telegram`: long polling, upsert пользователя, белый список `FAMILY_TELEGRAM_IDS`, индикатор «печатает…», ответ при ошибке LLM.
2. История диалога: последние ~10 сообщений пользователя из `chat_messages` (только текст user/assistant), TTL ~30 мин — для «последнее было…» и ответов на уточнения.
3. Последовательная обработка сообщений одного пользователя (mutex по `userID`), разные пользователи — параллельно.
4. `cmd/bot` + сервис `bot` в compose.
5. Eval-набор: таблица фраз → ожидаемые вызовы инструментов (все примеры из ТЗ + 15–20 своих: «вчера», «позавчера», «на прошлой неделе», «1.5к», «удали последнюю»). Запуск против реальной модели по флагу `-tags=eval`, по умолчанию пропускается.
6. Ручная проверка изоляции: два аккаунта Telegram.

**Результат:** бот проходит все сценарии ТЗ.

## День 4 — веб-панель

1. `web/`: Vite + React + TS, TanStack Query, UI-kit, API-клиент (типы по `openapi.yaml`).
2. Страница трат:
   - фильтры: член семьи (select из `/users`), период (date range + пресеты «неделя/месяц»), категория;
   - таблица: дата, член семьи, категория, сумма, комментарий, действия; пагинация; итог по выборке;
   - фильтры синхронизированы с URL query.
3. Модалка создания/редактирования с валидацией; подтверждение удаления.
4. Обработка загрузки/ошибок/пустого состояния.
5. Dockerfile фронта (сборка + nginx), прокси `/api` → `api:8080`, сервис `web` в compose.

**Результат:** полный CRUD в панели через `http://localhost:3000`.

## День 5 — полировка и сдача

1. Прогон с нуля: `git clone` → `.env` → `docker compose up` на чистой машине/volume.
2. Линтеры (`golangci-lint`, `eslint`), `go test ./...`, фикс замечаний.
3. README: описание, архитектурная схема, запуск, конфиг LLM (OpenAI / OpenRouter / Ollama), примеры фраз, допущения, ограничения, раздел о процессе разработки.
4. Скриншоты/GIF работы бота и панели.
5. Опционально при наличии времени: seed-данные для демо, CI (GitHub Actions: test + build), голосовые сообщения через Whisper-совместимый API.

## Порядок коммитов (ориентир)

`chore: project skeleton` → `feat(db): migrations and repositories` → `feat(api): expenses REST API` → `feat(llm): openai-compatible client` → `feat(agent): tool-calling agent` → `feat(bot): telegram adapter` → `test(agent): eval suite` → `feat(web): expenses dashboard` → `chore(deploy): compose with web` → `docs: README`.

## Открытые вопросы

| Вопрос | Решение по умолчанию |
|---|---|
| Какой LLM-провайдер доступен для демо? | OpenAI-совместимый; в README — примеры для OpenAI, OpenRouter, Ollama |
| Нужен ли белый список семьи? | Да, через конфиг; пустой — пускать всех |
| Хранить историю диалога в БД или в памяти? | В БД (переживает рестарт) |
| UI-kit фронта | Mantine (готовые таблица, date picker, модалки) |
