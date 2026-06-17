package db

import (
	"encoding/json"
	"errors"
	"fmt"
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
	var m sdb.ClaimAccess = 4

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
	if !strings.Contains(string(raw), `"access_slice":["READ"]`) {
		t.Fatalf("expected access_slice on ClaimObject, got %s", raw)
	}
	if !strings.Contains(string(raw), `"access_string":"READ"`) {
		t.Fatalf("expected access_string on ClaimObject, got %s", raw)
	}
}

func TestClaim_AccessVirtualFieldsEmit(t *testing.T) {
	c := sdb.Claim{
		ID:      uuid.New(),
		Name:    "Editor",
		Access:  sdb.ACCESS_WRITE | sdb.ACCESS_READ,
		OwnerID: uuid.New(),
		RootID:  uuid.New(),
	}
	raw, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"access_slice":["READ","WRITE"]`) {
		t.Fatalf("expected access_slice on Claim, got %s", raw)
	}
	if !strings.Contains(string(raw), `"access_string":"READ|WRITE"`) {
		t.Fatalf("expected access_string on Claim, got %s", raw)
	}
}

func TestUserClaim_AccessVirtualFieldsEmit(t *testing.T) {
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
	if !strings.Contains(string(raw), `"access_slice":["READ"]`) {
		t.Fatalf("expected access_slice on UserClaim, got %s", raw)
	}
	if !strings.Contains(string(raw), `"access_string":"READ"`) {
		t.Fatalf("expected access_string on UserClaim, got %s", raw)
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

func TestErrNotFound_DistinctSentinel(t *testing.T) {
	if sdb.ErrNotFound.Error() != "not found" {
		t.Fatalf("expected sentinel message 'not found', got %q", sdb.ErrNotFound.Error())
	}
	if errors.Is(sdb.ErrNotFound, sdb.ErrNoAccess) {
		t.Fatal("ErrNotFound must not match ErrNoAccess")
	}
	if errors.Is(sdb.ErrNoAccess, sdb.ErrNotFound) {
		t.Fatal("ErrNoAccess must not match ErrNotFound")
	}
	wrapped := fmt.Errorf("mutation access: %w", sdb.ErrNotFound)
	if !errors.Is(wrapped, sdb.ErrNotFound) {
		t.Fatal("wrapped ErrNotFound must satisfy errors.Is")
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

func TestClaimAccess_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name    string
		data    string
		want    sdb.ClaimAccess
		isError bool
	}{
		{
			name:    "Number: ACCESS_NONE (0)",
			data:    `0`,
			want:    sdb.ACCESS_NONE,
			isError: false,
		},
		{
			name:    "Number: Combined WRITE and READ (6)",
			data:    `6`,
			want:    sdb.ACCESS_WRITE | sdb.ACCESS_READ,
			isError: false,
		},
		{
			name:    "Number: Float error",
			data:    `6.5`,
			want:    0,
			isError: true,
		},

		{
			name:    "String: Single value (READ)",
			data:    `"READ"`,
			want:    sdb.ACCESS_READ,
			isError: false,
		},
		{
			name:    "String: Combined via pipe (WRITE|READ)",
			data:    `"WRITE|READ"`,
			want:    sdb.ACCESS_WRITE | sdb.ACCESS_READ,
			isError: false,
		},
		{
			name:    "String: Stringified numbers (2|4)",
			data:    `"2|4"`,
			want:    sdb.ACCESS_WRITE | sdb.ACCESS_READ,
			isError: false,
		},
		{
			name:    "String: Invalid mapping",
			data:    `"INVALID_VALUE"`,
			want:    0,
			isError: true,
		},

		{
			name:    "Array: Single value",
			data:    `["READ"]`,
			want:    sdb.ACCESS_READ,
			isError: false,
		},
		{
			name:    "Array: Combined via pipe format",
			data:    `["WRITE"|"READ"]`,
			want:    sdb.ACCESS_WRITE | sdb.ACCESS_READ,
			isError: false,
		},

		{
			name:    "Invalid Type: Boolean",
			data:    `true`,
			want:    0,
			isError: true,
		},
		{
			name:    "Empty data",
			data:    ``,
			want:    0,
			isError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got sdb.ClaimAccess
			err := got.UnmarshalJSON([]byte(tt.data))

			if tt.isError {
				if err == nil {
					t.Errorf("UnmarshalJSON() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("UnmarshalJSON() unexpected error: %v", err)
				return
			}

			if got != tt.want {
				t.Errorf("UnmarshalJSON() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestClaimAccess_Slice(t *testing.T) {
	tests := []struct {
		name string
		a    sdb.ClaimAccess
		want []string
	}{
		{
			name: "No access",
			a:    sdb.ACCESS_NONE,
			want: []string{},
		},
		{
			name: "Single access (READ)",
			a:    sdb.ACCESS_READ,
			want: []string{"READ"},
		},
		{
			name: "Multiple accesses (READ and WRITE)",
			a:    sdb.ACCESS_READ | sdb.ACCESS_WRITE,
			want: []string{"READ", "WRITE"},
		},
		{
			name: "ACCESS_ALL",
			a:    sdb.ACCESS_ALL,
			want: []string{"EXECUTE", "READ", "WRITE", "OWNER"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.a.Slice()

			if len(got) == 0 && len(tt.want) == 0 {
				return
			}

			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Slice() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestClaimAccess_PipeString(t *testing.T) {
	tests := []struct {
		name string
		a    sdb.ClaimAccess
		want string
	}{
		{
			name: "No access",
			a:    sdb.ACCESS_NONE,
			want: "",
		},
		{
			name: "Single access (WRITE)",
			a:    sdb.ACCESS_WRITE,
			want: "WRITE",
		},
		{
			name: "Multiple accesses",
			a:    sdb.ACCESS_READ | sdb.ACCESS_WRITE,
			want: "READ|WRITE",
		},
		{
			name: "ACCESS_ALL",
			a:    sdb.ACCESS_ALL,
			want: "EXECUTE|READ|WRITE|OWNER",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.a.PipeString(); got != tt.want {
				t.Errorf("PipeString() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestClaimAccess_PipeInt(t *testing.T) {
	tests := []struct {
		name string
		a    sdb.ClaimAccess
		want string
	}{
		{
			name: "No access",
			a:    sdb.ACCESS_NONE,
			want: "",
		},
		{
			name: "Single access (EXECUTE)",
			a:    sdb.ACCESS_EXECUTE,
			want: "8",
		},
		{
			name: "Multiple accesses",
			a:    sdb.ACCESS_READ | sdb.ACCESS_WRITE,
			want: "4|2",
		},
		{
			name: "ACCESS_ALL",
			a:    sdb.ACCESS_ALL,
			want: "8|4|2|1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.a.PipeInt(); got != tt.want {
				t.Errorf("PipeInt() = %v, want %v", got, tt.want)
			}
		})
	}
}
