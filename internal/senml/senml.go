package senml

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

type entry struct {
	Name      string   `json:"n"`
	Unit      string   `json:"u"`
	Value     *float64 `json:"v"`
	BoolValue *bool    `json:"vb"`
}

type record struct {
	BaseName string  `json:"bn"`
	BaseTime int64   `json:"bt"`
	Entries  []entry `json:"e"`
}

type Measurement struct {
	Name  string
	Unit  string
	Value any // float64 or bool
}

type Reading struct {
	BaseTime     int64
	Measurements []Measurement
}

func Parse(body []byte) (Reading, error) {
	var records []record
	if err := json.Unmarshal(body, &records); err != nil {
		return Reading{}, fmt.Errorf("invalid JSON: %w", err)
	}
	if len(records) == 0 {
		return Reading{}, ErrEmptyPayload
	}

	rec := records[0]
	if rec.BaseTime == 0 {
		return Reading{}, ErrMissingBaseTime
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
		return Reading{}, ErrEmptyEntries
	}

	return Reading{BaseTime: rec.BaseTime, Measurements: measurements}, nil
}
