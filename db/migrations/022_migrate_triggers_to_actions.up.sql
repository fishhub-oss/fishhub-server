-- 1. Insert one peripheral_action row per live trigger.
INSERT INTO actions (id, type, config)
SELECT
    gen_random_uuid(),
    'peripheral_action',
    jsonb_build_object(
        'peripheral_id', t.target_peripheral_id,
        'command',       t.action->>'action',
        'value',         t.action->'value'
    )
FROM triggers t
WHERE t.deleted_at IS NULL;

-- 2. Wire up action_triggers join rows.
INSERT INTO action_triggers (trigger_id, action_id)
SELECT t.id, a.id
FROM triggers t
JOIN actions a ON a.config->>'peripheral_id' = t.target_peripheral_id::text
WHERE t.deleted_at IS NULL
  AND a.type = 'peripheral_action';

-- 3. Drop old columns.
ALTER TABLE triggers DROP COLUMN target_peripheral_id;
ALTER TABLE triggers DROP COLUMN action;
