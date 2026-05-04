CREATE TABLE triggers (
    id                   UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id            UUID         NOT NULL REFERENCES devices(id),
    name                 TEXT         NOT NULL,
    enabled              BOOL         NOT NULL DEFAULT true,
    condition            JSONB        NOT NULL,
    target_peripheral_id UUID         NOT NULL REFERENCES peripherals(id),
    action               JSONB        NOT NULL,
    cooldown_s           INT          NOT NULL DEFAULT 60,
    deleted_at           TIMESTAMPTZ,
    created_at           TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX triggers_device_id_idx ON triggers (device_id) WHERE deleted_at IS NULL;
