CREATE TABLE trigger_events (
    id                UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    trigger_event_id  TEXT        NOT NULL UNIQUE,
    trigger_id        UUID        NOT NULL REFERENCES triggers(id),
    fired_at          TIMESTAMPTZ NOT NULL,
    received_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    readings          JSONB       NOT NULL DEFAULT '[]'
);

CREATE INDEX trigger_events_trigger_id_idx ON trigger_events (trigger_id);
CREATE INDEX trigger_events_fired_at_idx   ON trigger_events (fired_at DESC);
