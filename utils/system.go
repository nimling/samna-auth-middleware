package utils

import (
	"github.com/google/uuid"
)

func IsSystemApp(value any) bool {
	switch v := value.(type) {
	case string:
		if v == "" {
			return true
		}
		if v == Cfg().AppSlug {
			return true
		}
		parsed, err := uuid.Parse(v)
		if err == nil && parsed == uuid.Nil {
			return true
		}
		return false
	case uuid.UUID:
		return v == uuid.Nil
	default:
		return false
	}
}
