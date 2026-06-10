-- Phase C: Kafka outbox, integration audit, employee user/manager links.

ALTER TABLE erp_employees
    ADD COLUMN IF NOT EXISTS user_id UUID,
    ADD COLUMN IF NOT EXISTS manager_id UUID REFERENCES erp_employees(id);

CREATE UNIQUE INDEX IF NOT EXISTS erp_employees_user_id_idx
    ON erp_employees (user_id) WHERE user_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS erp_employees_manager_idx ON erp_employees (manager_id);

CREATE TABLE IF NOT EXISTS erp_integration_calls (
    id            BIGSERIAL PRIMARY KEY,
    target        TEXT NOT NULL,
    operation     TEXT NOT NULL,
    correlation   TEXT,
    status        TEXT NOT NULL CHECK (status IN ('ok', 'error', 'skipped')),
    request_body  JSONB,
    response_body JSONB,
    error_message TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS erp_integration_calls_target_idx
    ON erp_integration_calls (target, created_at DESC);

CREATE TABLE IF NOT EXISTS erp_event_outbox (
    id            BIGSERIAL PRIMARY KEY,
    kafka_topic   TEXT NOT NULL DEFAULT 'iag.operations',
    event_type    TEXT NOT NULL,
    event_key     TEXT,
    payload       JSONB NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    available_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    dispatched_at TIMESTAMPTZ,
    attempts      INT NOT NULL DEFAULT 0,
    last_error    TEXT
);

CREATE INDEX IF NOT EXISTS erp_event_outbox_due_idx
    ON erp_event_outbox (available_at) WHERE dispatched_at IS NULL;
