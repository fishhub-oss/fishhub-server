package sensors

import "context"

type DeviceStore interface {
	LookupByToken(ctx context.Context, token string) (DeviceInfo, error)
}

type TokenStore interface {
	CreateToken(ctx context.Context, userID string) (TokenResult, error)
}
