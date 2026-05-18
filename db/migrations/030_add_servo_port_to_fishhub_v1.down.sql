DELETE FROM device_model_ports
WHERE label = 'SERVO 1'
  AND pin   = 25
  AND model_id = (SELECT id FROM device_models WHERE slug = 'fishhub-v1');
