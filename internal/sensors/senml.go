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

type senmlRecord struct {
	BaseName    string   `json:"bn"`
	BaseTime    int64    `json:"bt"`
	Name        string   `json:"n"`
	Unit        string   `json:"u"`
	Value       *float64 `json:"v"`
	BoolValue   *bool    `json:"vb"`
	StringValue *string  `json:"vs"`
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
	if len(records) < 2 {
		return SenMLReading{}, ErrEmptyPayload
	}

	base := records[0]
	if base.BaseTime == 0 {
		return SenMLReading{}, ErrMissingBaseTime
	}

	var measurements []Measurement
	for _, r := range records[1:] {
		if r.Name == "" {
			continue
		}
		switch {
		case r.Value != nil:
			measurements = append(measurements, Measurement{Name: r.Name, Unit: r.Unit, Value: *r.Value})
		case r.BoolValue != nil:
			measurements = append(measurements, Measurement{Name: r.Name, Unit: r.Unit, Value: *r.BoolValue})
		case r.StringValue != nil:
			measurements = append(measurements, Measurement{Name: r.Name, Unit: r.Unit, Value: *r.StringValue})
		}
	}

	if len(measurements) == 0 {
		return SenMLReading{}, ErrEmptyEntries
	}

	return SenMLReading{BaseTime: base.BaseTime, Measurements: measurements}, nil
}
