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

	"github.com/fishhub-oss/fishhub-server/internal/outbox"
	"github.com/fishhub-oss/fishhub-server/internal/testutil"
)

// ── applyPatch ────────────────────────────────────────────────────────────────

func baseTrigger() Trigger {
	return Trigger{
		ID:              "t1",
		Name:            "original",
		Enabled:         true,
		Condition:       json.RawMessage(`{"op":"lt"}`),
		Action:          json.RawMessage(`{"action":"set","value":1.0}`),
		CooldownSeconds: 60,
	}
}

func TestApplyPatch_noPatch_preservesAll(t *testing.T) {
	cur := baseTrigger()
	u := applyPatch(cur, TriggerPatch{})
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
	if !bytes.Equal(u.Action, cur.Action) {
		t.Error("action changed")
	}
}

func TestApplyPatch_name(t *testing.T) {
	name := "updated"
	u := applyPatch(baseTrigger(), TriggerPatch{Name: &name})
	if u.Name != "updated" {
		t.Errorf("name: got %q want %q", u.Name, "updated")
	}
}

func TestApplyPatch_enabled(t *testing.T) {
	enabled := false
	u := applyPatch(baseTrigger(), TriggerPatch{Enabled: &enabled})
	if u.Enabled {
		t.Error("expected enabled=false")
	}
}

func TestApplyPatch_cooldown(t *testing.T) {
	cd := 120
	u := applyPatch(baseTrigger(), TriggerPatch{CooldownSeconds: &cd})
	if u.CooldownSeconds != 120 {
		t.Errorf("cooldown_s: got %d want 120", u.CooldownSeconds)
	}
}

func TestApplyPatch_condition(t *testing.T) {
	newCond := json.RawMessage(`{"op":"gt"}`)
	u := applyPatch(baseTrigger(), TriggerPatch{Condition: newCond})
	if !bytes.Equal(u.Condition, newCond) {
		t.Errorf("condition: got %s want %s", u.Condition, newCond)
	}
}

func TestApplyPatch_action(t *testing.T) {
	newAction := json.RawMessage(`{"action":"set_mode","mode":"automatic"}`)
	u := applyPatch(baseTrigger(), TriggerPatch{Action: newAction})
	if !bytes.Equal(u.Action, newAction) {
		t.Errorf("action: got %s want %s", u.Action, newAction)
	}
}

func TestApplyPatch_nilCondition_unchanged(t *testing.T) {
	// nil (absent) Condition must not clear the existing value.
	u := applyPatch(baseTrigger(), TriggerPatch{Condition: nil})
	if !bytes.Equal(u.Condition, baseTrigger().Condition) {
		t.Error("condition was cleared by nil patch")
	}
}

// ── service stubs ─────────────────────────────────────────────────────────────

var svcDiscardLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

type stubStore struct {
	created       Trigger
	createKindPin string
	createErr     error
	listed        []Trigger
	listErr       error
	got           Trigger
	getErr        error
	updated       Trigger
	updateKindPin string
	updateErr     error
	deleteErr     error
}

func (s *stubStore) Create(_ context.Context, _ *sql.Tx, _, _ string, _ TriggerCreate) (Trigger, string, error) {
	return s.created, s.createKindPin, s.createErr
}
func (s *stubStore) List(_ context.Context, _, _ string) ([]Trigger, error) {
	return s.listed, s.listErr
}
func (s *stubStore) Get(_ context.Context, _, _, _ string) (Trigger, error) {
	return s.got, s.getErr
}
func (s *stubStore) Update(_ context.Context, _ *sql.Tx, _, _, _ string, _ TriggerUpdate) (Trigger, string, error) {
	return s.updated, s.updateKindPin, s.updateErr
}
func (s *stubStore) Delete(_ context.Context, _ *sql.Tx, _, _, _ string) (Trigger, error) {
	return s.created, s.deleteErr
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
	// store.Get returns ErrNotFound → service returns before touching db
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
	store := &stubStore{created: baseTrigger(), createKindPin: "relay-14"}
	outboxStore := &stubOutbox{insertErr: errors.New("outbox down")}
	svc := NewService(db, store, outboxStore, svcDiscardLogger)

	_, err := svc.Create(context.Background(), "dev-1", "user-1", TriggerCreate{
		Name:      "test",
		Condition: json.RawMessage(`{}`),
		Action:    json.RawMessage(`{}`),
	})
	if err == nil {
		t.Fatal("expected error from outbox failure, got nil")
	}
}

func TestService_Update_mergeApplied_viaCapture(t *testing.T) {
	db := testutil.NewTestDB(t)

	cur := baseTrigger()
	newName := "renamed"
	cooldown := 30
	capture := &captureUpdateStore{got: cur, updateResult: cur, updateKindPin: "relay-14"}
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
	got           Trigger
	lastUpdate    TriggerUpdate
	updateResult  Trigger
	updateKindPin string
}

func (c *captureUpdateStore) Create(_ context.Context, _ *sql.Tx, _, _ string, _ TriggerCreate) (Trigger, string, error) {
	return Trigger{}, "", nil
}
func (c *captureUpdateStore) List(_ context.Context, _, _ string) ([]Trigger, error) {
	return nil, nil
}
func (c *captureUpdateStore) Get(_ context.Context, _, _, _ string) (Trigger, error) {
	return c.got, nil
}
func (c *captureUpdateStore) Update(_ context.Context, _ *sql.Tx, _, _, _ string, u TriggerUpdate) (Trigger, string, error) {
	c.lastUpdate = u
	return c.updateResult, c.updateKindPin, nil
}
func (c *captureUpdateStore) Delete(_ context.Context, _ *sql.Tx, _, _, _ string) (Trigger, error) {
	return Trigger{}, nil
}
