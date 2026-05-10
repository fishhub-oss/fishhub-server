ALTER TABLE devices
    ADD COLUMN model_id UUID REFERENCES device_models(id);

UPDATE devices
SET model_id = (SELECT id FROM device_models WHERE slug = 'fishhub-v1')
WHERE model_id IS NULL;

ALTER TABLE devices
    ALTER COLUMN model_id SET NOT NULL;
