package db

import (
	"fmt"

	"github.com/google/uuid"
	"github.com/nimling/samna-auth-middleware/models"
)

type QueryAccess struct {
	UserID uuid.UUID
}

func NewQueryAccess(userID uuid.UUID) QueryAccess {
	return QueryAccess{UserID: userID}
}

func (qa *QueryAccess) OrganizationWhere(accessLevel models.ClaimAccessLevel) string {
	return fmt.Sprintf(
		`o.id IN (SELECT object_id FROM samna_auth.active_object WHERE object_type = 'organization' AND claim_id IN (SELECT claim_id FROM samna_auth.active_claim WHERE user_id = '%[1]s' AND sa_access_level <= %[2]d))`,
		qa.UserID, accessLevel,
	)
}

func (qa *QueryAccess) LocationWhere(accessLevel models.ClaimAccessLevel) string {
	return fmt.Sprintf(
		`l.id IN (SELECT object_id FROM samna_auth.active_object WHERE object_type = 'location' AND claim_id IN (SELECT claim_id FROM samna_auth.active_claim WHERE user_id = '%[1]s' AND sa_access_level <= %[2]d))
OR
l.sa_owner_id IN (SELECT object_id FROM samna_auth.active_object WHERE object_type = 'organization' AND claim_id IN (SELECT claim_id FROM samna_auth.active_claim WHERE user_id = '%[1]s' AND sa_access_level <= %[2]d))`,
		qa.UserID, accessLevel,
	)
}

func (qa *QueryAccess) UserWhere(accessLevel models.ClaimAccessLevel) string {
	return fmt.Sprintf(
		`u.user_id = '%[1]s' OR u.user_id IN (SELECT target_user_id FROM samna_auth.active_users WHERE user_id = '%[1]s' AND sa_access_level <= %[2]d)`,
		qa.UserID, accessLevel,
	)
}

func (qa *QueryAccess) ClaimWhere(accessLevel models.ClaimAccessLevel) string {
	return fmt.Sprintf(
		`c.id IN (SELECT claim_id FROM samna_auth.active_claim WHERE user_id = '%[1]s' AND sa_access_level <= %[2]d) OR c.sa_owner_id IN (SELECT object_id FROM samna_auth.active_object WHERE object_type = 'organization' AND claim_id IN (SELECT claim_id FROM samna_auth.active_claim WHERE user_id = '%[1]s' AND sa_access_level <= %[2]d))`,
		qa.UserID, accessLevel,
	)
}

func (qa *QueryAccess) ObjectWhere(objectType string, alias string, accessLevel models.ClaimAccessLevel) string {
	return fmt.Sprintf(
		`%[3]s.id IN (SELECT object_id FROM samna_auth.active_object WHERE object_type = '%[4]s' AND claim_id IN (SELECT claim_id FROM samna_auth.active_claim WHERE user_id = '%[1]s' AND sa_access_level <= %[2]d))`,
		qa.UserID, accessLevel, alias, objectType,
	)
}
