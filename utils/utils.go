package utils

import (
	"os"

	"github.com/google/uuid"
)

func GetDebugUserID() *uuid.UUID {
	val, ok := os.LookupEnv("SAMNA_AUTH_DEBUG_USER_ID")
	if !ok || val == "" {
		return nil
	}
	id, err := uuid.Parse(val)
	if err != nil {
		return nil
	}
	return &id
}
