CREATE TABLE outbox_events (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_type            TEXT NOT NULL,
    payload               JSONB NOT NULL,
    status                TEXT NOT NULL DEFAULT 'pending',
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    claimed_at            TIMESTAMPTZ,
    claim_timeout_seconds INT NOT NULL DEFAULT 300,
    attempts              INT NOT NULL DEFAULT 0,
    last_error            TEXT
);

CREATE INDEX outbox_events_claimable ON outbox_events (event_type, created_at)
    WHERE status = 'pending' OR status = 'processing';
