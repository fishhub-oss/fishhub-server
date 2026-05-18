INSERT INTO device_model_ports (model_id, kind, label, pin)
SELECT m.id, 'servo_positional', 'SERVO 1', 25
FROM device_models m
WHERE m.slug = 'fishhub-v1'
ON CONFLICT DO NOTHING;
