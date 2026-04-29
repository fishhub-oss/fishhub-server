CREATE UNIQUE INDEX peripherals_device_pin_active_idx
    ON peripherals (device_id, pin)
    WHERE deleted_at IS NULL;
