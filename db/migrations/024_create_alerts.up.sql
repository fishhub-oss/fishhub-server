CREATE TYPE alert_severity AS ENUM ('info', 'warning', 'critical');

CREATE TABLE alerts (
    id           UUID           PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID           NOT NULL REFERENCES users(id),
    device_id    UUID           NOT NULL REFERENCES devices(id),
    trigger_id   UUID           NOT NULL REFERENCES triggers(id),
    event_id     UUID           NOT NULL REFERENCES trigger_events(id),
    severity     alert_severity NOT NULL,
    message      TEXT           NOT NULL,
    context      JSONB          NOT NULL DEFAULT '{}',
    created_at   TIMESTAMPTZ    NOT NULL DEFAULT now()
);

CREATE INDEX alerts_user_id_idx   ON alerts (user_id, created_at DESC);
CREATE INDEX alerts_device_id_idx ON alerts (device_id);
