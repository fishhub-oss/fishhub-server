ALTER TABLE peripherals
  DROP CONSTRAINT peripherals_control_mode_actuator_only,
  DROP COLUMN control_mode,
  DROP COLUMN category;
