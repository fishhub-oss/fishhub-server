ALTER TABLE provisioning_codes ALTER COLUMN user_id DROP NOT NULL;
ALTER TABLE provisioning_codes DROP COLUMN user_id;
ALTER TABLE provisioning_codes ALTER COLUMN device_id SET NOT NULL;
ALTER TABLE provisioning_codes ADD CONSTRAINT provisioning_codes_device_id_fkey FOREIGN KEY (device_id) REFERENCES devices(id);
