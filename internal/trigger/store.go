package trigger

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Store handles trigger persistence.
type Store interface {
	// Create inserts a new trigger, its peripheral_action row in actions, and the
	// action_triggers join row, all within tx.
	// Returns device.ErrNotFound if the device does not exist or is not owned by userID.
	Create(ctx context.Context, tx *sql.Tx, deviceID, userID string, p TriggerCreate) (Trigger, error)
	// List returns non-deleted triggers for the device owned by userID, with Actions hydrated.
	List(ctx context.Context, deviceID, userID string) ([]Trigger, error)
	// Get returns a single non-deleted trigger with Actions hydrated. Returns ErrNotFound if not reachable.
	Get(ctx context.Context, deviceID, userID, triggerID string) (Trigger, error)
	// Update replaces trigger fields and updates the existing actions row config within tx.
	// Returns ErrNotFound if the trigger does not exist or is not reachable by userID.
	Update(ctx context.Context, tx *sql.Tx, deviceID, userID, triggerID string, u TriggerUpdate) (Trigger, error)
	// Delete soft-deletes the trigger (sets deleted_at). Returns ErrNotFound if not reachable.
	Delete(ctx context.Context, tx *sql.Tx, deviceID, userID, triggerID string) (Trigger, error)
	// GetActions returns all actions for the given trigger.
	GetActions(ctx context.Context, triggerID string) ([]Action, error)
	// GetByID returns a trigger by ID regardless of device/user ownership. Returns ErrNotFound if not found.
	GetByID(ctx context.Context, triggerID string) (Trigger, error)
	// GetActionConfig returns the type and config for a single action by ID.
	GetActionConfig(ctx context.Context, actionID string) (Action, error)
}

type postgresStore struct {
	db *sql.DB
}

func NewStore(db *sql.DB) Store {
	return &postgresStore{db: db}
}

func (s *postgresStore) Create(ctx context.Context, tx *sql.Tx, deviceID, userID string, p TriggerCreate) (Trigger, error) {
	var t Trigger
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO triggers (device_id, name, condition, cooldown_s)
		VALUES ($1, $2, $3, $4)
		RETURNING id, device_id, name, enabled, condition, cooldown_s, created_at
	`, deviceID, p.Name, p.Condition, p.CooldownSeconds).Scan(
		&t.ID, &t.DeviceID, &t.Name, &t.Enabled,
		&t.Condition, &t.CooldownSeconds, &t.CreatedAt,
	); err != nil {
		return Trigger{}, fmt.Errorf("create trigger: insert trigger: %w", err)
	}

	for _, a := range p.Actions {
		var actionID string
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO actions (type, config)
			VALUES ($1, $2)
			RETURNING id
		`, a.Type, a.Config).Scan(&actionID); err != nil {
			return Trigger{}, fmt.Errorf("create trigger: insert action: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO action_triggers (trigger_id, action_id) VALUES ($1, $2)
		`, t.ID, actionID); err != nil {
			return Trigger{}, fmt.Errorf("create trigger: insert action_triggers: %w", err)
		}
		t.Actions = append(t.Actions, Action{ID: actionID, Type: a.Type, Config: a.Config})
	}
	return t, nil
}

func (s *postgresStore) List(ctx context.Context, deviceID, userID string) ([]Trigger, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT tr.id, tr.device_id, tr.name, tr.enabled, tr.condition,
		       tr.cooldown_s, tr.created_at
		FROM triggers tr
		JOIN devices d ON d.id = tr.device_id
		WHERE tr.device_id = $1
		  AND d.user_id = $2
		  AND tr.deleted_at IS NULL
		  AND d.deleted_at IS NULL
		ORDER BY tr.created_at ASC
	`, deviceID, userID)
	if err != nil {
		return nil, fmt.Errorf("list triggers: query: %w", err)
	}
	defer rows.Close()

	var triggers []Trigger
	var ids []string
	idIndex := map[string]int{}
	for rows.Next() {
		var t Trigger
		if err := rows.Scan(
			&t.ID, &t.DeviceID, &t.Name, &t.Enabled,
			&t.Condition, &t.CooldownSeconds, &t.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("list triggers: scan: %w", err)
		}
		idIndex[t.ID] = len(triggers)
		ids = append(ids, t.ID)
		triggers = append(triggers, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(triggers) == 0 {
		return []Trigger{}, nil
	}

	if err := s.hydrateActions(ctx, s.db, triggers, ids, idIndex); err != nil {
		return nil, fmt.Errorf("list triggers: %w", err)
	}
	return triggers, nil
}

func (s *postgresStore) Get(ctx context.Context, deviceID, userID, triggerID string) (Trigger, error) {
	var t Trigger
	err := s.db.QueryRowContext(ctx, `
		SELECT tr.id, tr.device_id, tr.name, tr.enabled, tr.condition,
		       tr.cooldown_s, tr.created_at
		FROM triggers tr
		JOIN devices d ON d.id = tr.device_id
		WHERE tr.id = $1
		  AND tr.device_id = $2
		  AND d.user_id = $3
		  AND tr.deleted_at IS NULL
		  AND d.deleted_at IS NULL
	`, triggerID, deviceID, userID).Scan(
		&t.ID, &t.DeviceID, &t.Name, &t.Enabled,
		&t.Condition, &t.CooldownSeconds, &t.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Trigger{}, ErrNotFound
	}
	if err != nil {
		return Trigger{}, fmt.Errorf("get trigger: %w", err)
	}

	triggers := []Trigger{t}
	if err := s.hydrateActions(ctx, s.db, triggers, []string{t.ID}, map[string]int{t.ID: 0}); err != nil {
		return Trigger{}, fmt.Errorf("get trigger: %w", err)
	}
	return triggers[0], nil
}

func (s *postgresStore) Update(ctx context.Context, tx *sql.Tx, deviceID, userID, triggerID string, u TriggerUpdate) (Trigger, error) {
	var t Trigger
	err := tx.QueryRowContext(ctx, `
		UPDATE triggers tr
		SET name       = $1,
		    condition  = $2,
		    cooldown_s = $3,
		    enabled    = $4
		FROM devices d
		WHERE tr.id        = $5
		  AND tr.device_id = $6
		  AND d.id         = tr.device_id
		  AND d.user_id    = $7
		  AND tr.deleted_at IS NULL
		  AND d.deleted_at  IS NULL
		RETURNING tr.id, tr.device_id, tr.name, tr.enabled, tr.condition,
		          tr.cooldown_s, tr.created_at
	`, u.Name, u.Condition, u.CooldownSeconds, u.Enabled,
		triggerID, deviceID, userID,
	).Scan(
		&t.ID, &t.DeviceID, &t.Name, &t.Enabled,
		&t.Condition, &t.CooldownSeconds, &t.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Trigger{}, ErrNotFound
	}
	if err != nil {
		return Trigger{}, fmt.Errorf("update trigger: %w", err)
	}

	// Replace the full action set: delete existing join rows and orphaned actions,
	// then insert the new set.
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM actions a
		USING action_triggers at
		WHERE at.action_id = a.id AND at.trigger_id = $1
	`, t.ID); err != nil {
		return Trigger{}, fmt.Errorf("update trigger: delete old actions: %w", err)
	}

	t.Actions = t.Actions[:0]
	for _, a := range u.Actions {
		var actionID string
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO actions (type, config) VALUES ($1, $2) RETURNING id
		`, a.Type, a.Config).Scan(&actionID); err != nil {
			return Trigger{}, fmt.Errorf("update trigger: insert action: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO action_triggers (trigger_id, action_id) VALUES ($1, $2)
		`, t.ID, actionID); err != nil {
			return Trigger{}, fmt.Errorf("update trigger: insert action_triggers: %w", err)
		}
		t.Actions = append(t.Actions, Action{ID: actionID, Type: a.Type, Config: a.Config})
	}
	return t, nil
}

func (s *postgresStore) Delete(ctx context.Context, tx *sql.Tx, deviceID, userID, triggerID string) (Trigger, error) {
	var t Trigger
	err := tx.QueryRowContext(ctx, `
		UPDATE triggers tr
		SET deleted_at = now()
		FROM devices d
		WHERE tr.id        = $1
		  AND tr.device_id = $2
		  AND d.id         = tr.device_id
		  AND d.user_id    = $3
		  AND tr.deleted_at IS NULL
		  AND d.deleted_at  IS NULL
		RETURNING tr.id, tr.device_id, tr.name, tr.enabled, tr.condition,
		          tr.cooldown_s, tr.created_at
	`, triggerID, deviceID, userID).Scan(
		&t.ID, &t.DeviceID, &t.Name, &t.Enabled,
		&t.Condition, &t.CooldownSeconds, &t.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Trigger{}, ErrNotFound
	}
	if err != nil {
		return Trigger{}, fmt.Errorf("delete trigger: %w", err)
	}
	return t, nil
}

func (s *postgresStore) GetActions(ctx context.Context, triggerID string) ([]Action, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT a.id, a.type, a.config
		FROM actions a
		JOIN action_triggers at ON at.action_id = a.id
		WHERE at.trigger_id = $1
		ORDER BY a.created_at ASC
	`, triggerID)
	if err != nil {
		return nil, fmt.Errorf("get actions: %w", err)
	}
	defer rows.Close()

	var actions []Action
	for rows.Next() {
		var a Action
		if err := rows.Scan(&a.ID, &a.Type, &a.Config); err != nil {
			return nil, fmt.Errorf("get actions: scan: %w", err)
		}
		actions = append(actions, a)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if actions == nil {
		return []Action{}, nil
	}
	return actions, nil
}

func (s *postgresStore) GetByID(ctx context.Context, triggerID string) (Trigger, error) {
	var t Trigger
	err := s.db.QueryRowContext(ctx, `
		SELECT tr.id, tr.device_id, d.user_id, tr.name, tr.enabled, tr.condition, tr.cooldown_s, tr.created_at
		FROM triggers tr
		JOIN devices d ON d.id = tr.device_id
		WHERE tr.id = $1 AND tr.deleted_at IS NULL AND d.deleted_at IS NULL
	`, triggerID).Scan(
		&t.ID, &t.DeviceID, &t.UserID, &t.Name, &t.Enabled,
		&t.Condition, &t.CooldownSeconds, &t.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Trigger{}, ErrNotFound
	}
	if err != nil {
		return Trigger{}, fmt.Errorf("get trigger by id: %w", err)
	}
	return t, nil
}

func (s *postgresStore) GetActionConfig(ctx context.Context, actionID string) (Action, error) {
	var a Action
	err := s.db.QueryRowContext(ctx, `
		SELECT id, type, config FROM actions WHERE id = $1
	`, actionID).Scan(&a.ID, &a.Type, &a.Config)
	if errors.Is(err, sql.ErrNoRows) {
		return Action{}, ErrNotFound
	}
	if err != nil {
		return Action{}, fmt.Errorf("get action config: %w", err)
	}
	return a, nil
}

// hydrateActions fetches all actions for the given trigger IDs and populates
// the Actions slice on each element of triggers (matched by idIndex).
func (s *postgresStore) hydrateActions(ctx context.Context, q interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}, triggers []Trigger, ids []string, idIndex map[string]int) error {
	placeholders := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = id
	}
	query := buildInQuery(`
		SELECT at.trigger_id, a.id, a.type, a.config
		FROM action_triggers at
		JOIN actions a ON a.id = at.action_id
		WHERE at.trigger_id IN (`, len(ids), `)
		ORDER BY a.created_at ASC
	`)
	rows, err := q.QueryContext(ctx, query, placeholders...)
	if err != nil {
		return fmt.Errorf("hydrate actions: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var triggerID string
		var a Action
		if err := rows.Scan(&triggerID, &a.ID, &a.Type, &a.Config); err != nil {
			return fmt.Errorf("hydrate actions: scan: %w", err)
		}
		if idx, ok := idIndex[triggerID]; ok {
			triggers[idx].Actions = append(triggers[idx].Actions, a)
		}
	}
	return rows.Err()
}

// buildInQuery builds a parameterised IN clause query.
// prefix ends just before the opening paren; suffix starts just after.
func buildInQuery(prefix string, n int, suffix string) string {
	buf := make([]byte, 0, len(prefix)+len(suffix)+n*4)
	buf = append(buf, prefix...)
	for i := range n {
		if i > 0 {
			buf = append(buf, ',')
		}
		buf = fmt.Appendf(buf, "$%d", i+1)
	}
	buf = append(buf, suffix...)
	return string(buf)
}
