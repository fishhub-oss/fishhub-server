package auth

import "time"

type User struct {
	ID          string
	Email       string
	Provider    string
	ProviderSub string
	CreatedAt   time.Time
}
