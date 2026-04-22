DROP INDEX devices_user_id_status_idx;
DROP INDEX devices_status_idx;
ALTER TABLE devices DROP COLUMN status;
