CREATE TABLE device_firmware_updates (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id       UUID        NOT NULL REFERENCES devices(id),
    desired_version TEXT        NOT NULL,
    nonce           TEXT        NOT NULL,
    status          TEXT        NOT NULL DEFAULT 'pending',
    last_error      TEXT,
    requested_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX device_firmware_updates_device_status_idx
    ON device_firmware_updates (device_id, status);
