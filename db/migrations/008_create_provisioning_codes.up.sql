CREATE TABLE provisioning_codes (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code       CHAR(6) NOT NULL UNIQUE,
    device_id  UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    used_at    TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
