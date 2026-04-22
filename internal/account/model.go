package account

import "time"

type Account struct {
	ID        string
	UserID    string
	Email     string
	Name      string
	CreatedAt time.Time
	UpdatedAt time.Time
}
