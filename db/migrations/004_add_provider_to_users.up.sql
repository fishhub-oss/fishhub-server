ALTER TABLE users
    ADD COLUMN provider     TEXT NOT NULL DEFAULT 'local',
    ADD COLUMN provider_sub TEXT NOT NULL DEFAULT '';

ALTER TABLE users
    ADD CONSTRAINT users_provider_sub_unique UNIQUE (provider, provider_sub);
