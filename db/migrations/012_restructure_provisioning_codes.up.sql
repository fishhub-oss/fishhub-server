ALTER TABLE provisioning_codes ALTER COLUMN device_id DROP NOT NULL;
ALTER TABLE provisioning_codes DROP CONSTRAINT provisioning_codes_device_id_fkey;
ALTER TABLE provisioning_codes ADD COLUMN user_id UUID REFERENCES users(id);
UPDATE provisioning_codes pc SET user_id = d.user_id FROM devices d WHERE d.id = pc.device_id;
ALTER TABLE provisioning_codes ALTER COLUMN user_id SET NOT NULL;
