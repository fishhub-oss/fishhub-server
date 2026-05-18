-- A physical pin can host multiple port kind definitions (e.g. servo_cr and
-- servo_positional on the same GPIO). Port-in-use enforcement is already
-- handled at the peripherals level via peripherals_port_id_active_idx.
ALTER TABLE device_model_ports DROP CONSTRAINT device_model_ports_model_id_pin_key;
