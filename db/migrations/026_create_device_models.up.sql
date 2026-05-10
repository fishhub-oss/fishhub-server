CREATE TABLE device_models (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slug       VARCHAR(64)  NOT NULL UNIQUE,
    name       VARCHAR(128) NOT NULL,
    created_at TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE TABLE device_model_ports (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    model_id   UUID        NOT NULL REFERENCES device_models(id),
    kind       VARCHAR(32) NOT NULL,
    label      VARCHAR(32) NOT NULL,
    pin        INT         NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE (model_id, pin),
    UNIQUE (model_id, kind, label)
);

-- Seed fishhub-v1
INSERT INTO device_models (slug, name)
VALUES ('fishhub-v1', 'FishHub v1')
ON CONFLICT (slug) DO NOTHING;

INSERT INTO device_model_ports (model_id, kind, label, pin)
SELECT m.id, v.kind, v.label, v.pin
FROM device_models m,
     (VALUES
         ('ds18b20', 'TEMP',     4),
         ('relay',   'RELAY 1', 16),
         ('relay',   'RELAY 2', 17),
         ('relay',   'RELAY 3', 26),
         ('relay',   'RELAY 4', 27),
         ('analog',  'ANALOG 1', 32),
         ('analog',  'ANALOG 2', 33),
         ('analog',  'ANALOG 3', 34),
         ('analog',  'ANALOG 4', 35)
     ) AS v(kind, label, pin)
WHERE m.slug = 'fishhub-v1'
ON CONFLICT DO NOTHING;
