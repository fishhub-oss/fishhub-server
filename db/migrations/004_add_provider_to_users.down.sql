ALTER TABLE users
    DROP CONSTRAINT users_provider_sub_unique,
    DROP COLUMN provider,
    DROP COLUMN provider_sub;
