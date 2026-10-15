-- migrations/00001_init_schema.sql

-- +goose Up
CREATE TABLE users (
    telegram_id BIGINT PRIMARY KEY,
    username    TEXT,
    first_name  TEXT NOT NULL DEFAULT '',
    last_name   TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE expenses (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    BIGINT NOT NULL REFERENCES users (telegram_id) ON DELETE CASCADE,
    -- сумма в копейках
    amount     BIGINT NOT NULL CHECK (amount > 0),
    category   TEXT NOT NULL CHECK (category IN (
        'food', 'transport', 'housing', 'entertainment', 'health', 'shopping', 'other'
    )),
    spent_at   DATE NOT NULL,
    comment    TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX expenses_user_spent_at_idx ON expenses (user_id, spent_at DESC, created_at DESC);
CREATE INDEX expenses_spent_at_idx ON expenses (spent_at DESC, created_at DESC);

-- короткая история диалога с ботом (контекст для ИИ-агента)
CREATE TABLE chat_messages (
    id         BIGSERIAL PRIMARY KEY,
    user_id    BIGINT NOT NULL REFERENCES users (telegram_id) ON DELETE CASCADE,
    role       TEXT NOT NULL CHECK (role IN ('user', 'assistant')),
    content    TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX chat_messages_user_created_idx ON chat_messages (user_id, created_at DESC);

-- +goose Down
DROP TABLE chat_messages;
DROP TABLE expenses;
DROP TABLE users;
