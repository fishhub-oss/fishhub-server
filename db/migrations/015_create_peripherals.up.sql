CREATE TABLE peripherals (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id  UUID        NOT NULL REFERENCES devices(id),
    name       TEXT        NOT NULL,
    kind       TEXT        NOT NULL,
    pin        INT         NOT NULL,
    schedule   JSONB,
    deleted_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX peripherals_device_name_active_idx
    ON peripherals (device_id, name)
    WHERE deleted_at IS NULL;
