CREATE TABLE devices (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL REFERENCES users(id),
    name       TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
