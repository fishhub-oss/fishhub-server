package measurement

import (
	"testing"
	"time"
)

func TestReduceLatest(t *testing.T) {
	t1 := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC) // newest
	t2 := time.Date(2026, 6, 12, 9, 55, 0, 0, time.UTC)
	t3 := time.Date(2026, 6, 12, 9, 50, 0, 0, time.UTC)

	tests := []struct {
		name       string
		rows       []map[string]any
		wantTime   time.Time
		wantValues map[string]any
		wantNil    bool
	}{
		{
			name:    "no rows",
			rows:    nil,
			wantNil: true,
		},
		{
			name: "only reserved columns",
			rows: []map[string]any{
				{"time": t1, "device_id": "d", "user_id": "u"},
			},
			wantNil: true,
		},
		{
			name: "single row, single field",
			rows: []map[string]any{
				{"time": t1, "device_id": "d", "user_id": "u", "ds18b20-4/temperature": float64(23.4)},
			},
			wantTime:   t1,
			wantValues: map[string]any{"ds18b20-4/temperature": float64(23.4)},
		},
		{
			name: "newest non-null per field wins",
			rows: []map[string]any{
				// newest first
				{"time": t1, "ds18b20-4/temperature": float64(23.4)},
				{"time": t2, "ds18b20-4/temperature": float64(20.0)},
			},
			wantTime:   t1,
			wantValues: map[string]any{"ds18b20-4/temperature": float64(23.4)},
		},
		{
			name: "fields spread across rows are all captured",
			rows: []map[string]any{
				{"time": t1, "ds18b20-4/temperature": float64(23.4)},
				{"time": t2, "analog-32/ph": float64(7.2)},
				{"time": t3, "ds18b20-4/temperature": float64(20.0), "analog-32/ph": float64(7.0)},
			},
			wantTime: t1,
			wantValues: map[string]any{
				"ds18b20-4/temperature": float64(23.4), // from t1, not the older t3
				"analog-32/ph":          float64(7.2),  // from t2, not the older t3
			},
		},
		{
			name: "null gap: latest non-null is taken from an older row",
			rows: []map[string]any{
				// newest row has the field present but nil (sparse columnar write)
				{"time": t1, "ds18b20-4/temperature": nil},
				{"time": t2, "ds18b20-4/temperature": float64(21.1)},
			},
			wantTime:   t1, // timestamp is still the newest row
			wantValues: map[string]any{"ds18b20-4/temperature": float64(21.1)},
		},
		{
			name: "bool coerced to 1/0",
			rows: []map[string]any{
				{"time": t1, "relay-16/state": true, "relay-17/state": false},
			},
			wantTime:   t1,
			wantValues: map[string]any{"relay-16/state": 1, "relay-17/state": 0},
		},
		{
			name: "string field preserved",
			rows: []map[string]any{
				{"time": t1, "light-26/source": "schedule"},
			},
			wantTime:   t1,
			wantValues: map[string]any{"light-26/source": "schedule"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := reduceLatest(tt.rows)
			if tt.wantNil {
				if got != nil {
					t.Fatalf("expected nil, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatal("expected a Point, got nil")
			}
			if !got.Timestamp.Equal(tt.wantTime) {
				t.Errorf("timestamp: want %v, got %v", tt.wantTime, got.Timestamp)
			}
			if len(got.Values) != len(tt.wantValues) {
				t.Errorf("values: want %d entries %v, got %d entries %v",
					len(tt.wantValues), tt.wantValues, len(got.Values), got.Values)
			}
			for k, want := range tt.wantValues {
				if got.Values[k] != want {
					t.Errorf("values[%q]: want %v, got %v", k, want, got.Values[k])
				}
			}
		})
	}
}
