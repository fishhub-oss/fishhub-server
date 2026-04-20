package sensors

import (
	"encoding/json"
	"errors"
	"fmt"
)

var (
	ErrEmptyPayload    = errors.New("payload must contain at least one record")
	ErrMissingBaseTime = errors.New("missing or zero base time (bt)")
	ErrEmptyEntries    = errors.New("payload must contain at least one entry")
)

type senmlEntry struct {
	Name      string   `json:"n"`
	Unit      string   `json:"u"`
	Value     *float64 `json:"v"`
	BoolValue *bool    `json:"vb"`
}

type senmlRecord struct {
	BaseName string       `json:"bn"`
	BaseTime int64        `json:"bt"`
	Entries  []senmlEntry `json:"e"`
}

type Measurement struct {
	Name  string
	Unit  string
	Value any // float64 or bool
}

type SenMLReading struct {
	BaseTime     int64
	Measurements []Measurement
}

func ParseSenML(body []byte) (SenMLReading, error) {
	var records []senmlRecord
	if err := json.Unmarshal(body, &records); err != nil {
		return SenMLReading{}, fmt.Errorf("invalid JSON: %w", err)
	}
	if len(records) == 0 {
		return SenMLReading{}, ErrEmptyPayload
	}

	rec := records[0]
	if rec.BaseTime == 0 {
		return SenMLReading{}, ErrMissingBaseTime
	}

	var measurements []Measurement
	for _, e := range rec.Entries {
		if e.Name == "" {
			continue
		}
		switch {
		case e.Value != nil:
			measurements = append(measurements, Measurement{Name: e.Name, Unit: e.Unit, Value: *e.Value})
		case e.BoolValue != nil:
			measurements = append(measurements, Measurement{Name: e.Name, Unit: e.Unit, Value: *e.BoolValue})
		}
	}

	if len(measurements) == 0 {
		return SenMLReading{}, ErrEmptyEntries
	}

	return SenMLReading{BaseTime: rec.BaseTime, Measurements: measurements}, nil
}
