package provisioning

import (
	"context"
	"log/slog"
)

// Service orchestrates device provisioning from the user side.
type Service struct {
	store  Store
	logger *slog.Logger
}

func NewService(store Store, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{store: store, logger: logger}
}

// Provision returns an existing unused provisioning code or creates a new one.
func (s *Service) Provision(ctx context.Context, userID string) (string, error) {
	code, err := s.store.GetOrCreateCode(ctx, userID)
	if err != nil {
		s.logger.Error("provision: get or create code", "user_id", userID, "error", err)
	}
	return code, err
}
