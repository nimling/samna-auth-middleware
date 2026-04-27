package client

import (
	"context"
	"fmt"
	"os"

	"github.com/google/uuid"
	"github.com/nimling/samna-auth-middleware/db"
)

func getDebugUserID() *uuid.UUID {
	val, ok := os.LookupEnv("SAUTH_DEBUG_USER_ID")
	if !ok || val == "" {
		return nil
	}
	id, err := uuid.Parse(val)
	if err != nil {
		return nil
	}
	return &id
}

func resolveAppID(ctx context.Context) (uuid.UUID, error) {
	var id uuid.UUID
	err := db.Instance().NewRaw(
		`SELECT id FROM claimius.samna_app WHERE slug = ? AND sa_deleted_at IS NULL LIMIT 1`,
		os.Getenv("SAUTH_APP_SLUG"),
	).Scan(ctx, &id)
	if err != nil {
		return uuid.Nil, fmt.Errorf("resolve app id: %w", err)
	}
	return id, nil
}
