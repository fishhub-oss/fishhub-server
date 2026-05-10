DROP INDEX IF EXISTS peripherals_port_id_active_idx;
ALTER TABLE peripherals DROP COLUMN IF EXISTS port_id;
