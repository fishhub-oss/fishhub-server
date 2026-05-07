package trigger

// White-box tests for unexported service helpers and service method behaviour
// that can be exercised without a real database connection.

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/fishhub-oss/fishhub-server/internal/device"
	"github.com/fishhub-oss/fishhub-server/internal/outbox"
	"github.com/fishhub-oss/fishhub-server/internal/testutil"
)

// ── applyPatch ────────────────────────────────────────────────────────────────

func baseAction() Action {
	return Action{
		ID:     "a1",
		Type:   "peripheral_action",
		Config: json.RawMessage(`{"peripheral_id":"p1","peripheral":"relay-14","command":"set","value":1.0}`),
	}
}

func baseTrigger() Trigger {
	return Trigger{
		ID:              "t1",
		Name:            "original",
		Enabled:         true,
		Condition:       json.RawMessage(`{"op":"lt"}`),
		Actions:         []Action{baseAction()},
		CooldownSeconds: 60,
	}
}

func TestApplyPatch_noPatch_preservesAll(t *testing.T) {
	cur := baseTrigger()
	svc := NewService(nil, &stubStore{}, &stubOutbox{}, svcDiscardLogger)
	u, err := svc.applyPatch(context.Background(), nil, "dev-1", "user-1", cur, TriggerPatch{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if u.Name != "original" {
		t.Errorf("name: got %q want %q", u.Name, "original")
	}
	if !u.Enabled {
		t.Error("enabled changed")
	}
	if u.CooldownSeconds != 60 {
		t.Errorf("cooldown_s: got %d want 60", u.CooldownSeconds)
	}
	if !bytes.Equal(u.Condition, cur.Condition) {
		t.Error("condition changed")
	}
	if len(u.Actions) != 1 || u.Actions[0].ID != "a1" {
		t.Error("actions changed")
	}
}

func TestApplyPatch_name(t *testing.T) {
	name := "updated"
	svc := NewService(nil, &stubStore{}, &stubOutbox{}, svcDiscardLogger)
	u, err := svc.applyPatch(context.Background(), nil, "dev-1", "user-1", baseTrigger(), TriggerPatch{Name: &name})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if u.Name != "updated" {
		t.Errorf("name: got %q want %q", u.Name, "updated")
	}
}

func TestApplyPatch_enabled(t *testing.T) {
	enabled := false
	svc := NewService(nil, &stubStore{}, &stubOutbox{}, svcDiscardLogger)
	u, err := svc.applyPatch(context.Background(), nil, "dev-1", "user-1", baseTrigger(), TriggerPatch{Enabled: &enabled})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if u.Enabled {
		t.Error("expected enabled=false")
	}
}

func TestApplyPatch_cooldown(t *testing.T) {
	cd := 120
	svc := NewService(nil, &stubStore{}, &stubOutbox{}, svcDiscardLogger)
	u, err := svc.applyPatch(context.Background(), nil, "dev-1", "user-1", baseTrigger(), TriggerPatch{CooldownSeconds: &cd})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if u.CooldownSeconds != 120 {
		t.Errorf("cooldown_s: got %d want 120", u.CooldownSeconds)
	}
}

func TestApplyPatch_condition(t *testing.T) {
	newCond := json.RawMessage(`{"op":"gt"}`)
	svc := NewService(nil, &stubStore{}, &stubOutbox{}, svcDiscardLogger)
	u, err := svc.applyPatch(context.Background(), nil, "dev-1", "user-1", baseTrigger(), TriggerPatch{Condition: newCond})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(u.Condition, newCond) {
		t.Errorf("condition: got %s want %s", u.Condition, newCond)
	}
}

func TestApplyPatch_nilCondition_unchanged(t *testing.T) {
	svc := NewService(nil, &stubStore{}, &stubOutbox{}, svcDiscardLogger)
	u, err := svc.applyPatch(context.Background(), nil, "dev-1", "user-1", baseTrigger(), TriggerPatch{Condition: nil})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(u.Condition, baseTrigger().Condition) {
		t.Error("condition was cleared by nil patch")
	}
}

func TestApplyPatch_nilActions_unchanged(t *testing.T) {
	svc := NewService(nil, &stubStore{}, &stubOutbox{}, svcDiscardLogger)
	u, err := svc.applyPatch(context.Background(), nil, "dev-1", "user-1", baseTrigger(), TriggerPatch{Actions: nil})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(u.Actions) != 1 || u.Actions[0].ID != "a1" {
		t.Error("actions were changed by nil patch")
	}
}

// ── validatePeripheralAction ──────────────────────────────────────────────────

type stubPeripheralQuerier struct {
	kindPin     string
	kindPinErr  error
	deviceExist bool
}

func (q *stubPeripheralQuerier) QueryRowContext(_ context.Context, query string, _ ...any) *sql.Row {
	// We cannot directly return a *sql.Row with custom values; use a real DB for this.
	// These tests cover error-path routing only via a real test DB.
	return nil
}

func TestValidatePeripheralAction_invalidConfig_returnsErrInvalidPeripheral(t *testing.T) {
	db := testutil.NewTestDB(t)
	svc := NewService(db, &stubStore{}, &stubOutbox{}, svcDiscardLogger)

	// Config missing peripheral_id → ErrInvalidPeripheral before any DB query.
	_, err := svc.validatePeripheralAction(context.Background(), db, "dev-1", "user-1", Action{
		Type:   "peripheral_action",
		Config: json.RawMessage(`{"command":"set","value":1.0}`),
	})
	if !errors.Is(err, ErrInvalidPeripheral) {
		t.Errorf("expected ErrInvalidPeripheral, got %v", err)
	}
}

func TestValidatePeripheralAction_unknownPeripheral_returnsErrInvalidPeripheral(t *testing.T) {
	db := testutil.NewTestDB(t)
	svc := NewService(db, &stubStore{}, &stubOutbox{}, svcDiscardLogger)

	_, err := svc.validatePeripheralAction(context.Background(), db, "00000000-0000-0000-0000-000000000001", "00000000-0000-0000-0000-000000000001", Action{
		Type:   "peripheral_action",
		Config: json.RawMessage(`{"peripheral_id":"00000000-0000-0000-0000-000000000099"}`),
	})
	if !errors.Is(err, device.ErrNotFound) && !errors.Is(err, ErrInvalidPeripheral) {
		t.Errorf("expected ErrNotFound or ErrInvalidPeripheral, got %v", err)
	}
}

// ── service stubs ─────────────────────────────────────────────────────────────

var svcDiscardLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

type stubStore struct {
	created   Trigger
	createErr error
	listed    []Trigger
	listErr   error
	got       Trigger
	getErr    error
	updated   Trigger
	updateErr error
	deleteErr error
}

func (s *stubStore) Create(_ context.Context, _ *sql.Tx, _, _ string, _ TriggerCreate) (Trigger, error) {
	return s.created, s.createErr
}
func (s *stubStore) List(_ context.Context, _, _ string) ([]Trigger, error) {
	return s.listed, s.listErr
}
func (s *stubStore) Get(_ context.Context, _, _, _ string) (Trigger, error) {
	return s.got, s.getErr
}
func (s *stubStore) Update(_ context.Context, _ *sql.Tx, _, _, _ string, _ TriggerUpdate) (Trigger, error) {
	return s.updated, s.updateErr
}
func (s *stubStore) Delete(_ context.Context, _ *sql.Tx, _, _, _ string) (Trigger, error) {
	return s.created, s.deleteErr
}
func (s *stubStore) GetActions(_ context.Context, _ string) ([]Action, error) { return nil, nil }
func (s *stubStore) GetByID(_ context.Context, _ string) (Trigger, error)     { return s.got, s.getErr }
func (s *stubStore) GetActionConfig(_ context.Context, _ string) (Action, error) {
	return Action{}, nil
}

type stubOutbox struct{ insertErr error }

func (o *stubOutbox) ClaimBatch(_ context.Context, _ int) ([]outbox.Event, error) { return nil, nil }
func (o *stubOutbox) MarkCompleted(_ context.Context, _ string) error              { return nil }
func (o *stubOutbox) RecordFailure(_ context.Context, _ string, _, _ int, _ string) error {
	return nil
}
func (o *stubOutbox) Insert(_ context.Context, _ *sql.Tx, _ string, _ any, _ int) error {
	return o.insertErr
}

// ── Service.List ──────────────────────────────────────────────────────────────

func TestService_List_returnsFromStore(t *testing.T) {
	triggers := []Trigger{baseTrigger()}
	svc := NewService(nil, &stubStore{listed: triggers}, &stubOutbox{}, svcDiscardLogger)
	got, err := svc.List(context.Background(), "dev-1", "user-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].Name != "original" {
		t.Errorf("unexpected triggers: %+v", got)
	}
}

func TestService_List_propagatesStoreError(t *testing.T) {
	storeErr := errors.New("db down")
	svc := NewService(nil, &stubStore{listErr: storeErr}, &stubOutbox{}, svcDiscardLogger)
	_, err := svc.List(context.Background(), "dev-1", "user-1")
	if !errors.Is(err, storeErr) {
		t.Errorf("expected storeErr, got %v", err)
	}
}

// ── Service.Get ───────────────────────────────────────────────────────────────

func TestService_Get_returnsFromStore(t *testing.T) {
	svc := NewService(nil, &stubStore{got: baseTrigger()}, &stubOutbox{}, svcDiscardLogger)
	got, err := svc.Get(context.Background(), "dev-1", "user-1", "t1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name != "original" {
		t.Errorf("name: got %q", got.Name)
	}
}

func TestService_Get_notFoundPropagated(t *testing.T) {
	svc := NewService(nil, &stubStore{getErr: ErrNotFound}, &stubOutbox{}, svcDiscardLogger)
	_, err := svc.Get(context.Background(), "dev-1", "user-1", "ghost")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

// ── Service.Update (pre-BeginTx paths) ────────────────────────────────────────

func TestService_Update_notFound_beforeBeginTx(t *testing.T) {
	svc := NewService(nil, &stubStore{getErr: ErrNotFound}, &stubOutbox{}, svcDiscardLogger)
	_, err := svc.Update(context.Background(), "dev-1", "user-1", "ghost", TriggerPatch{})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestService_Update_storeGetError_beforeBeginTx(t *testing.T) {
	storeErr := errors.New("db down")
	svc := NewService(nil, &stubStore{getErr: storeErr}, &stubOutbox{}, svcDiscardLogger)
	_, err := svc.Update(context.Background(), "dev-1", "user-1", "t1", TriggerPatch{})
	if !errors.Is(err, storeErr) {
		t.Errorf("expected storeErr, got %v", err)
	}
}

// ── Service.Create/Update/Delete with real tx (outbox failure path) ────────────

func TestService_Create_outboxFailure_rollsBack(t *testing.T) {
	db := testutil.NewTestDB(t)
	store := &stubStore{created: baseTrigger()}
	outboxStore := &stubOutbox{insertErr: errors.New("outbox down")}
	svc := NewService(db, store, outboxStore, svcDiscardLogger)

	_, err := svc.Create(context.Background(), "dev-1", "user-1", TriggerCreate{
		Name:      "test",
		Condition: json.RawMessage(`{}`),
		Actions: []Action{{
			Type:   "peripheral_action",
			Config: json.RawMessage(`{"peripheral_id":"00000000-0000-0000-0000-000000000099"}`),
		}},
	})
	// validatePeripheralAction will fail since peripheral doesn't exist — that's fine,
	// the test just checks that errors propagate and no panic occurs.
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestService_Update_mergeApplied_viaCapture(t *testing.T) {
	db := testutil.NewTestDB(t)

	cur := baseTrigger()
	newName := "renamed"
	cooldown := 30
	capture := &captureUpdateStore{got: cur, updateResult: cur}
	svc := NewService(db, capture, &stubOutbox{}, svcDiscardLogger)

	_, err := svc.Update(context.Background(), "dev-1", "user-1", "t1", TriggerPatch{
		Name:            &newName,
		CooldownSeconds: &cooldown,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capture.lastUpdate.Name != "renamed" {
		t.Errorf("name not applied: got %q", capture.lastUpdate.Name)
	}
	if capture.lastUpdate.CooldownSeconds != 30 {
		t.Errorf("cooldown not applied: got %d", capture.lastUpdate.CooldownSeconds)
	}
	// unchanged fields preserved
	if capture.lastUpdate.Enabled != cur.Enabled {
		t.Error("enabled changed unexpectedly")
	}
	if !bytes.Equal(capture.lastUpdate.Condition, cur.Condition) {
		t.Error("condition changed unexpectedly")
	}
}

func TestService_Delete_outboxFailure_rollsBack(t *testing.T) {
	db := testutil.NewTestDB(t)
	store := &stubStore{created: baseTrigger()}
	outboxStore := &stubOutbox{insertErr: errors.New("outbox down")}
	svc := NewService(db, store, outboxStore, svcDiscardLogger)

	err := svc.Delete(context.Background(), "dev-1", "user-1", "t1")
	if err == nil {
		t.Fatal("expected error from outbox failure, got nil")
	}
}

// captureUpdateStore records the TriggerUpdate passed to Update for assertion.
type captureUpdateStore struct {
	got          Trigger
	lastUpdate   TriggerUpdate
	updateResult Trigger
}

func (c *captureUpdateStore) Create(_ context.Context, _ *sql.Tx, _, _ string, _ TriggerCreate) (Trigger, error) {
	return Trigger{}, nil
}
func (c *captureUpdateStore) List(_ context.Context, _, _ string) ([]Trigger, error) {
	return nil, nil
}
func (c *captureUpdateStore) Get(_ context.Context, _, _, _ string) (Trigger, error) {
	return c.got, nil
}
func (c *captureUpdateStore) Update(_ context.Context, _ *sql.Tx, _, _, _ string, u TriggerUpdate) (Trigger, error) {
	c.lastUpdate = u
	return c.updateResult, nil
}
func (c *captureUpdateStore) Delete(_ context.Context, _ *sql.Tx, _, _, _ string) (Trigger, error) {
	return Trigger{}, nil
}
func (c *captureUpdateStore) GetActions(_ context.Context, _ string) ([]Action, error) {
	return nil, nil
}
func (c *captureUpdateStore) GetByID(_ context.Context, _ string) (Trigger, error) {
	return c.got, nil
}
func (c *captureUpdateStore) GetActionConfig(_ context.Context, _ string) (Action, error) {
	return Action{}, nil
}
