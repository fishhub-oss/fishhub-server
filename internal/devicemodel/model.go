package devicemodel

import "time"

type Port struct {
	ID        string
	ModelID   string
	Kind      string
	Label     string
	Pin       int
	CreatedAt time.Time
}

type DeviceModel struct {
	ID        string
	Slug      string
	Name      string
	Ports     []Port
	CreatedAt time.Time
}
