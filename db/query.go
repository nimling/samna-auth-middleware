package db

import (
	"fmt"

	"github.com/google/uuid"
)

func NewQueryAccess(userID uuid.UUID) QueryAccess {
	return QueryAccess{UserID: userID}
}

func (qa *QueryAccess) UserWhere(accessLevel ClaimAccessLevel) string {
	return fmt.Sprintf(
		`u.user_id = '%[1]s' OR u.user_id IN (SELECT user_id FROM samna_auth.get_users('%[1]s', %[2]d))`,
		qa.UserID, accessLevel,
	)
}

func (qa *QueryAccess) ClaimWhere(accessLevel ClaimAccessLevel) string {
	return fmt.Sprintf(
		`c.id IN (SELECT claim_id FROM samna_auth.claim_access('%[1]s', NULL, %[2]d))`,
		qa.UserID, accessLevel,
	)
}

var objectAliases = map[string]string{
	"organization":  "o",
	"location":      "l",
	"bookable":      "bo",
	"bookable-type": "bt",
	"bookable_type": "bt",
	"booking":       "b",
	"timeslot":      "t",
	"checkin":       "c",
	"map":           "m",
	"registration":  "r",
	"asset":         "a",
	"audit":         "a",
	"activity":      "a",
	"action":        "ac",
	"action_object": "ao",
	"ai_request":    "air",
	"code":          "c",
	"capability":    "c",
}

func (qa *QueryAccess) ObjectWhere(objectType string, accessLevel ClaimAccessLevel) string {
	if objectType == "organization" || objectType == "location" || objectType == "user" {
		panic(fmt.Sprintf("use dedicated query functions for %s", objectType))
	}

	alias, ok := objectAliases[objectType]
	if !ok {
		alias = objectType[:1]
	}
	return fmt.Sprintf(
		`%[3]s.id IN (SELECT object_id FROM samna_auth.get_objects('%[1]s', '%[4]s', %[2]d))`,
		qa.UserID, accessLevel, alias, objectType,
	)
}
