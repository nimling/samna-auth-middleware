package hook

import (
	"encoding/json"
	"fmt"
	"time"
)

func NewUserPayload(et EventType, user User) (*Payload, error) {
	return newPayload(et, EntityTypeUser, user)
}

func NewOrganizationPayload(et EventType, org Organization) (*Payload, error) {
	return newPayload(et, EntityTypeOrganization, org)
}

func NewLocationPayload(et EventType, loc Location) (*Payload, error) {
	return newPayload(et, EntityTypeLocation, loc)
}

func NewClaimPayload(et EventType, claim Claim) (*Payload, error) {
	return newPayload(et, EntityTypeClaim, claim)
}

func NewClaimObjectPayload(et EventType, claim ClaimObject) (*Payload, error) {
	return newPayload(et, EntityTypeObjectClaim, claim)
}

func NewUserClaimPayload(et EventType, user UserClaim) (*Payload, error) {
	return newPayload(et, EntityTypeUserClaim, user)
}

func newPayload[T any](et EventType, ent EntityType, data T) (*Payload, error) {
	rawData, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("marshal %s payload: %w", ent, err)
	}

	return &Payload{
		EventType:  et,
		EntityType: ent,
		Timestamp:  time.Now().UTC(),
		Data:       rawData,
	}, nil
}
