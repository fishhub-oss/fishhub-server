ALTER TABLE devices
    ADD COLUMN status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'active'));

CREATE INDEX devices_status_idx ON devices (status);
CREATE INDEX devices_user_id_status_idx ON devices (user_id, status);
