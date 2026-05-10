ALTER TABLE peripherals
    ADD COLUMN port_id UUID REFERENCES device_model_ports(id);

CREATE UNIQUE INDEX peripherals_port_id_active_idx
    ON peripherals (port_id)
    WHERE deleted_at IS NULL AND port_id IS NOT NULL;
