ALTER TABLE peripherals
  ADD COLUMN category TEXT NOT NULL DEFAULT 'sensor'
    CHECK (category IN ('sensor', 'actuator')),
  ADD COLUMN control_mode TEXT
    CHECK (control_mode IS NULL OR control_mode IN ('automatic', 'manual'));

ALTER TABLE peripherals
  ADD CONSTRAINT peripherals_control_mode_actuator_only
    CHECK (category = 'actuator' OR control_mode IS NULL);

UPDATE peripherals SET category = 'actuator', control_mode = 'automatic'
WHERE kind = 'relay';
