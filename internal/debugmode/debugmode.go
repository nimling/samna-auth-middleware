package debugmode

import (
	"os"

	"github.com/google/uuid"
)

// GetUserID returns the SAUTH_DEBUG_USER_ID env value parsed as a UUID, or
// nil when unset, empty, or unparseable. This package lives under internal/
// so consumers of this module cannot import it; debug mode is an
// implementation detail of the middleware.
func GetUserID() *uuid.UUID {
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
