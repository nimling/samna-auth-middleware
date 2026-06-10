package db

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
	sdb "github.com/nimling/samna-auth-middleware/db"
)

func TestClaim_CreatorEmbedJSONRoundTrip(t *testing.T) {
	first := "Ada"
	c := sdb.Claim{
		ID:      uuid.New(),
		Name:    "Editor",
		Access:  6,
		OwnerID: uuid.New(),
		RootID:  uuid.New(),
		Creator: &sdb.Creator{
			ID:        uuid.New(),
			UserID:    uuid.New(),
			FirstName: &first,
			Email:     "ada@example.com",
		},
	}
	raw, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"access":6`) {
		t.Fatalf("expected access field on Claim, got %s", raw)
	}
	if !strings.Contains(string(raw), `"creator":{`) {
		t.Fatalf("expected creator object on Claim, got %s", raw)
	}
	var back sdb.Claim
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.Access != 6 {
		t.Fatalf("Access round-trip lost, got %d", back.Access)
	}
	if back.Creator == nil || back.Creator.Email != "ada@example.com" {
		t.Fatalf("Creator round-trip lost, got %+v", back.Creator)
	}
}

func TestClaimObject_NoOverlayShadowsAccessBitmask(t *testing.T) {
	m := 4
	co := sdb.ClaimObject{
		ID:      uuid.New(),
		ClaimID: uuid.New(),
		Access:  &m,
		OwnerID: uuid.New(),
		RootID:  uuid.New(),
	}
	raw, err := json.Marshal(co)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"access":4`) {
		t.Fatalf("expected access bitmask on ClaimObject, got %s", raw)
	}
}

func TestUserClaim_AccessAndCreatorEmbedsEmit(t *testing.T) {
	uc := sdb.UserClaim{
		ID:        uuid.New(),
		ClaimID:   uuid.New(),
		UserID:    uuid.New(),
		OwnerID:   uuid.New(),
		CreatedBy: uuid.New(),
		Access: &sdb.AccessClaim{
			Access:  4,
			OwnerID: uuid.New(),
			RootID:  uuid.New(),
		},
		Creator: &sdb.Creator{
			ID:     uuid.New(),
			UserID: uuid.New(),
			Email:  "ada@example.com",
		},
	}
	raw, err := json.Marshal(uc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"access":{`) {
		t.Fatalf("expected access overlay object on UserClaim, got %s", raw)
	}
	if !strings.Contains(string(raw), `"creator":{`) {
		t.Fatalf("expected creator overlay object on UserClaim, got %s", raw)
	}
}

func TestUserRelation_AccessOverlayEmits(t *testing.T) {
	r := sdb.UserRelation{
		ID:         uuid.New(),
		UserID:     uuid.New(),
		ObjectType: sdb.OBJECT_TYPE_USER,
		ObjectID:   uuid.New(),
		Access: &sdb.AccessClaim{
			Access:  4,
			OwnerID: uuid.New(),
			RootID:  uuid.New(),
		},
	}
	raw, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"access":{`) {
		t.Fatalf("expected access overlay on UserRelation, got %s", raw)
	}
}

func TestUserRelation_ObjectTypeConstantResolves(t *testing.T) {
	got, err := sdb.ResolveObjectType("user_relation")
	if err != nil {
		t.Fatalf("ResolveObjectType: %v", err)
	}
	if got != sdb.OBJECT_TYPE_USER_RELATION {
		t.Fatalf("expected %s, got %s", sdb.OBJECT_TYPE_USER_RELATION, got)
	}
}

func TestCreateVirtualClaimParams_NoRootID(t *testing.T) {
	rt := reflect.TypeOf(sdb.CreateVirtualClaimParams{})
	if _, ok := rt.FieldByName("RootID"); ok {
		t.Fatalf("CreateVirtualClaimParams must not carry RootID; derived from OwnerID in middleware SQL")
	}
	wanted := map[string]bool{
		"Name": true, "Description": true, "Access": true,
		"OwnerID": true, "ObjectID": true, "ObjectType": true, "TargetUserID": true,
	}
	for i := 0; i < rt.NumField(); i++ {
		name := rt.Field(i).Name
		if !wanted[name] {
			t.Fatalf("unexpected field %s on CreateVirtualClaimParams", name)
		}
		delete(wanted, name)
	}
	if len(wanted) > 0 {
		t.Fatalf("missing fields on CreateVirtualClaimParams: %v", wanted)
	}
}
