package senml

import (
	"encoding/json"
	"errors"
	"fmt"
)

var (
	ErrEmptyPayload       = errors.New("payload must contain at least one record")
	ErrMissingBaseTime    = errors.New("missing or zero base time (bt)")
	ErrMissingTemperature = errors.New("no temperature entry found in payload")
)

type Entry struct {
	Name  string  `json:"n"`
	Unit  string  `json:"u"`
	Value float64 `json:"v"`
}

type Record struct {
	BaseName string  `json:"bn"`
	BaseTime int64   `json:"bt"`
	Entries  []Entry `json:"e"`
}

type Reading struct {
	Temperature float64
	BaseTime    int64
}

func Parse(body []byte) (Reading, error) {
	var records []Record
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

	for _, e := range rec.Entries {
		if e.Name == "temperature" {
			return Reading{Temperature: e.Value, BaseTime: rec.BaseTime}, nil
		}
	}
	return Reading{}, ErrMissingTemperature
}
