package alert

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/fishhub-oss/fishhub-server/internal/alerts"
	"github.com/fishhub-oss/fishhub-server/internal/queue"
	"github.com/fishhub-oss/fishhub-server/internal/trigger"
)

// ── stubs ─────────────────────────────────────────────────────────────────────

type stubAlertStore struct {
	created alerts.Alert
}

func (s *stubAlertStore) Create(_ context.Context, a alerts.Alert) (alerts.Alert, error) {
	s.created = a
	return a, nil
}

func (s *stubAlertStore) ListByUser(_ context.Context, _ string, _ int) ([]alerts.Alert, error) {
	return nil, nil
}

type stubTriggerStore struct {
	trig   trigger.Trigger
	action trigger.Action
}

func (s *stubTriggerStore) Create(_ context.Context, _ *sql.Tx, _, _ string, _ trigger.TriggerCreate) (trigger.Trigger, error) {
	return trigger.Trigger{}, nil
}
func (s *stubTriggerStore) List(_ context.Context, _, _ string) ([]trigger.Trigger, error) {
	return nil, nil
}
func (s *stubTriggerStore) Get(_ context.Context, _, _, _ string) (trigger.Trigger, error) {
	return trigger.Trigger{}, nil
}
func (s *stubTriggerStore) Update(_ context.Context, _ *sql.Tx, _, _, _ string, _ trigger.TriggerUpdate) (trigger.Trigger, error) {
	return trigger.Trigger{}, nil
}
func (s *stubTriggerStore) Delete(_ context.Context, _ *sql.Tx, _, _, _ string) (trigger.Trigger, error) {
	return trigger.Trigger{}, nil
}
func (s *stubTriggerStore) GetActions(_ context.Context, _ string) ([]trigger.Action, error) {
	return nil, nil
}
func (s *stubTriggerStore) GetByID(_ context.Context, _ string) (trigger.Trigger, error) {
	return s.trig, nil
}
func (s *stubTriggerStore) GetActionConfig(_ context.Context, _ string) (trigger.Action, error) {
	return s.action, nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

func makeJob(t *testing.T, severity, msgTemplate string) (queue.Job, *stubAlertStore, *stubTriggerStore) {
	t.Helper()
	actionCfg, _ := json.Marshal(alertActionConfig{
		Severity:        severity,
		MessageTemplate: msgTemplate,
	})
	jobPayload, _ := json.Marshal(queue.AlertJobPayload{
		ActionID:  "action-1",
		EventID:   "event-1",
		TriggerID: "trigger-1",
		FiredAt:   time.Date(2025, 5, 5, 14, 0, 0, 0, time.UTC),
		Readings:  []queue.Reading{{Peripheral: "ds18b20-4/temperature", Value: 22.5}},
	})
	alertStore := &stubAlertStore{}
	triggerStore := &stubTriggerStore{
		trig:   trigger.Trigger{UserID: "user-1", DeviceID: "device-1"},
		action: trigger.Action{ID: "action-1", Type: "alert", Config: actionCfg},
	}
	return queue.Job{ID: "job-1", Type: "alert", Payload: jobPayload}, alertStore, triggerStore
}

// ── tests ─────────────────────────────────────────────────────────────────────

func TestProcessor_Process_invalidSeverity_returnsErrorAndNoWrite(t *testing.T) {
	job, alertStore, triggerStore := makeJob(t, "catastrophic", "{{.Value}}")
	p := NewProcessor(alertStore, triggerStore)

	err := p.Process(context.Background(), job)
	if err == nil {
		t.Fatal("expected error for invalid severity, got nil")
	}
	if alertStore.created.ID != "" {
		t.Error("expected no alert to be written on invalid severity")
	}
}

func TestProcessor_Process_createsAlertWithRenderedMessage(t *testing.T) {
	job, alertStore, triggerStore := makeJob(t, "warning", "{{.Peripheral}} is {{.Value}}")
	p := NewProcessor(alertStore, triggerStore)

	if err := p.Process(context.Background(), job); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if alertStore.created.Severity != "warning" {
		t.Errorf("severity: got %q, want %q", alertStore.created.Severity, "warning")
	}
	if alertStore.created.Message != "ds18b20-4/temperature is 22.5" {
		t.Errorf("message: got %q, want %q", alertStore.created.Message, "ds18b20-4/temperature is 22.5")
	}
	if alertStore.created.UserID != "user-1" {
		t.Errorf("user_id: got %q, want %q", alertStore.created.UserID, "user-1")
	}
	if alertStore.created.DeviceID != "device-1" {
		t.Errorf("device_id: got %q, want %q", alertStore.created.DeviceID, "device-1")
	}
	if alertStore.created.EventID != "event-1" {
		t.Errorf("event_id: got %q, want %q", alertStore.created.EventID, "event-1")
	}
}
