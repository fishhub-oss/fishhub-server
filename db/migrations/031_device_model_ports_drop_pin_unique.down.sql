ALTER TABLE device_model_ports ADD CONSTRAINT device_model_ports_model_id_pin_key UNIQUE (model_id, pin);
