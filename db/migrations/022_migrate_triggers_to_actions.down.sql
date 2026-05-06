-- WARNING: best-effort dev rollback only — not safe for production.
-- Re-add columns.
ALTER TABLE triggers ADD COLUMN target_peripheral_id UUID REFERENCES peripherals(id);
ALTER TABLE triggers ADD COLUMN action JSONB;

-- Restore from actions (best-effort).
UPDATE triggers t
SET
    target_peripheral_id = (a.config->>'peripheral_id')::uuid,
    action = jsonb_build_object(
        'action', a.config->>'command',
        'value',  a.config->'value'
    )
FROM action_triggers at
JOIN actions a ON a.id = at.action_id
WHERE at.trigger_id = t.id
  AND a.type = 'peripheral_action';

DELETE FROM action_triggers;
DELETE FROM actions;
