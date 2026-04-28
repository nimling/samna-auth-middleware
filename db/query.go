package db

import (
	"fmt"

	"github.com/google/uuid"
)

func UserWhere(userID uuid.UUID, accessLevel ClaimAccessLevel) string {
	return fmt.Sprintf(
		`(user_id = '%[1]s' OR user_id IN (SELECT user_id FROM claimius.get_users('%[1]s', claimius.get_disciple_app_id(), %[2]d))) AND user_id NOT IN (SELECT user_id FROM claimius.samna_user WHERE type = 'system' AND sa_deleted_at IS NULL)`,
		userID, accessLevel,
	)
}

func ClaimWhere(userID uuid.UUID, accessLevel ClaimAccessLevel) string {
	return fmt.Sprintf(
		`id IN (SELECT id FROM claimius.get_claims('%[1]s', claimius.get_disciple_app_id(), %[2]d))`,
		userID, accessLevel,
	)
}

func ObjectWhere(userID uuid.UUID, objectType string, accessLevel ClaimAccessLevel) string {
	return fmt.Sprintf(
		`id IN (SELECT object_id FROM claimius.get_objects('%[1]s', claimius.get_disciple_app_id(), '%[3]s', %[2]d))`,
		userID, accessLevel, objectType,
	)
}

func AuditWhere(userID uuid.UUID, accessLevel ClaimAccessLevel) string {
	return fmt.Sprintf(
		`id IN (SELECT id FROM claimius.get_audit('%[1]s', claimius.get_disciple_app_id(), %[2]d))`,
		userID, accessLevel,
	)
}
