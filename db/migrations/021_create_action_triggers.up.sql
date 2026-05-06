CREATE TABLE action_triggers (
    trigger_id UUID NOT NULL REFERENCES triggers(id) ON DELETE CASCADE,
    action_id  UUID NOT NULL REFERENCES actions(id)  ON DELETE CASCADE,
    PRIMARY KEY (trigger_id, action_id)
);

CREATE INDEX action_triggers_trigger_id_idx ON action_triggers (trigger_id);
