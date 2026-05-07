package alert

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"text/template"
	"time"

	"github.com/fishhub-oss/fishhub-server/internal/alerts"
	"github.com/fishhub-oss/fishhub-server/internal/queue"
	"github.com/fishhub-oss/fishhub-server/internal/trigger"
)

var validSeverities = map[string]bool{
	"info":     true,
	"warning":  true,
	"critical": true,
}

type alertActionConfig struct {
	Severity        string `json:"severity"`
	MessageTemplate string `json:"message_template"`
}

type templateData struct {
	Peripheral string
	Value      float64
	FiredAt    time.Time
}

// Processor processes "alert" jobs by writing an alert row to Postgres.
type Processor struct {
	alerts   alerts.Store
	triggers trigger.Store
}

func NewProcessor(alertStore alerts.Store, triggerStore trigger.Store) *Processor {
	return &Processor{alerts: alertStore, triggers: triggerStore}
}

func (p *Processor) Type() string { return "alert" }

func (p *Processor) Process(ctx context.Context, job queue.Job) error {
	var payload queue.AlertJobPayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return fmt.Errorf("alert processor: unmarshal payload: %w", err)
	}

	action, err := p.triggers.GetActionConfig(ctx, payload.ActionID)
	if err != nil {
		return fmt.Errorf("alert processor: get action config: %w", err)
	}

	var cfg alertActionConfig
	if err := json.Unmarshal(action.Config, &cfg); err != nil {
		return fmt.Errorf("alert processor: unmarshal action config: %w", err)
	}

	if !validSeverities[cfg.Severity] {
		return fmt.Errorf("alert processor: invalid severity %q", cfg.Severity)
	}

	trig, err := p.triggers.GetByID(ctx, payload.TriggerID)
	if err != nil {
		return fmt.Errorf("alert processor: get trigger: %w", err)
	}

	alertCtx, message, err := renderAlert(cfg.MessageTemplate, payload.Readings, payload.FiredAt)
	if err != nil {
		return fmt.Errorf("alert processor: render template: %w", err)
	}

	_, err = p.alerts.Create(ctx, alerts.Alert{
		UserID:    trig.UserID,
		DeviceID:  trig.DeviceID,
		TriggerID: payload.TriggerID,
		EventID:   payload.EventID,
		Severity:  cfg.Severity,
		Message:   message,
		Context:   alertCtx,
	})
	return err
}

func renderAlert(tmplStr string, readings []queue.Reading, firedAt time.Time) (map[string]any, string, error) {
	var peripheral string
	var value float64
	if len(readings) > 0 {
		peripheral = readings[0].Peripheral
		value = readings[0].Value
	}

	tmpl, err := template.New("message").Parse(tmplStr)
	if err != nil {
		return nil, "", fmt.Errorf("parse template: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, templateData{
		Peripheral: peripheral,
		Value:      value,
		FiredAt:    firedAt,
	}); err != nil {
		return nil, "", fmt.Errorf("execute template: %w", err)
	}

	alertCtx := map[string]any{
		"peripheral": peripheral,
		"value":      value,
		"fired_at":   firedAt,
	}
	return alertCtx, buf.String(), nil
}
