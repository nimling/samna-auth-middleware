package echo

import (
	"os"

	"github.com/google/uuid"
)

func getDebugUserID() *uuid.UUID {
	if id := getEnvUUID("SAUTH_DEBUG_USER_ID"); id != nil {
		return id
	}
	if os.Getenv("SAUTH_DEBUG_CLIENT_SECRET") == "" {
		return nil
	}
	return getEnvUUID("SAUTH_DEBUG_CLIENT_ID")
}

func getEnvUUID(envKey string) *uuid.UUID {
	val := os.Getenv(envKey)
	if val == "" {
		return nil
	}
	id, err := uuid.Parse(val)
	if err != nil {
		return nil
	}
	return &id
}
