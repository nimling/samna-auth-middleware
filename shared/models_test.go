package shared_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
	sdb "github.com/nimling/samna-auth-middleware/shared"
	"github.com/uptrace/bun"
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

func TestSamnaClient_WriteRoundTrip(t *testing.T) {
	owner := uuid.New()
	c := sdb.SamnaClient{Name: "Deploy key", OwnerID: owner}
	raw, err := c.MarshalWrite()
	if err != nil {
		t.Fatalf("marshal write: %v", err)
	}
	if !strings.Contains(string(raw), `"name":"Deploy key"`) || !strings.Contains(string(raw), fmt.Sprintf(`"owner_id":"%s"`, owner)) {
		t.Fatalf("expected the name and the owner on the write body, got %s", raw)
	}
	id, clientID := uuid.New(), uuid.New()
	answer := fmt.Sprintf(`{"id":"%s","name":"Deploy key","client_id":"%s","status":"active","owner_id":"%s","secret":"only-once"}`, id, clientID, owner)
	if err := c.UnmarshalWrite([]byte(answer)); err != nil {
		t.Fatalf("unmarshal write: %v", err)
	}
	if c.ID != id || c.ClientID != clientID || c.Secret == nil || *c.Secret != "only-once" {
		t.Fatalf("the prophet's answer did not land on the model, got %+v", c)
	}
	if c.PatchBody() != nil {
		t.Fatalf("a model bound from no request carries no patch body, got %v", c.PatchBody())
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
			name:    "Array: Combined names",
			data:    `["WRITE","READ"]`,
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

func TestAccessClaim_ObjectsJSONRoundTrip(t *testing.T) {
	empty := sdb.AccessClaim{UserObjectID: uuid.New()}
	rawEmpty, err := json.Marshal(empty)
	if err != nil {
		t.Fatalf("marshal empty: %v", err)
	}
	if strings.Contains(string(rawEmpty), "objects") {
		t.Fatalf("expected objects omitted when empty, got %s", rawEmpty)
	}

	scope := json.RawMessage(`{"page":{"time-report":{"_access":"READ|EXECUTE"}}}`)
	withObjects := sdb.AccessClaim{
		UserObjectID: uuid.New(),
		Objects: []sdb.ClaimObject{
			{ID: uuid.New(), ClaimID: uuid.New(), Scope: &scope},
		},
	}
	raw, err := json.Marshal(withObjects)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"objects":[`) {
		t.Fatalf("expected objects array on AccessClaim, got %s", raw)
	}
	var back sdb.AccessClaim
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(back.Objects) != 1 {
		t.Fatalf("objects round-trip lost, got %+v", back.Objects)
	}
	if back.Objects[0].Scope == nil {
		t.Fatalf("expected claim_object scope to round-trip")
	}
}

func TestObjectType_ClaimiusTypesAreValidated(t *testing.T) {
	if got, err := sdb.ResolveObjectType("organization"); err != nil || got != sdb.OBJECT_TYPE_ORGANIZATION {
		t.Fatalf("expected the bare claimius name to resolve, got %q, %v", got, err)
	}
	if got, err := sdb.ResolveObjectType(sdb.OBJECT_TYPE_AUDIT); err != nil || got != sdb.OBJECT_TYPE_AUDIT {
		t.Fatalf("expected a registered claimius type to pass, got %q, %v", got, err)
	}
	if _, err := sdb.ResolveObjectType("claimius.bogus"); err == nil {
		t.Fatal("expected a claimius type the schema does not register to be refused")
	}
	var o sdb.ObjectType
	if err := json.Unmarshal([]byte(`"claimius.bogus"`), &o); err == nil {
		t.Fatal("expected the wire to refuse an unknown claimius type")
	}
}

func TestObjectType_ConsumerTypePassesThroughJSON(t *testing.T) {
	for _, consumer := range []string{"inventory.device", "device"} {
		var co sdb.ClaimObject
		if err := json.Unmarshal([]byte(`{"object_type":"`+consumer+`"}`), &co); err != nil {
			t.Fatalf("unmarshal %q: %v", consumer, err)
		}
		if co.ObjectType == nil || string(*co.ObjectType) != consumer {
			t.Fatalf("expected %q in untouched, got %v", consumer, co.ObjectType)
		}
		value, err := co.ObjectType.Value()
		if err != nil || value != consumer {
			t.Fatalf("expected %q to bind untouched, got %v, %v", consumer, value, err)
		}
		raw, err := json.Marshal(co.ObjectType)
		if err != nil || string(raw) != `"`+consumer+`"` {
			t.Fatalf("expected %q out untouched, got %s, %v", consumer, raw, err)
		}
	}
}

func TestObjectType_ValidateIsTheConsumersOptIn(t *testing.T) {
	if err := sdb.ObjectType("device").Validate(); err == nil {
		t.Fatal("expected an unqualified type to fail the consumer's check")
	}
	if err := sdb.ObjectType("inventory.device").Validate(); err != nil {
		t.Fatalf("expected a qualified type to pass: %v", err)
	}
	if err := sdb.ObjectType("inventory.device").Validate("inventory.tag"); err == nil {
		t.Fatal("expected a type outside the consumer's list to fail")
	}
	if err := sdb.ObjectType("inventory.device").Validate("inventory.tag", "inventory.device"); err != nil {
		t.Fatalf("expected a listed type to pass: %v", err)
	}
}

func TestObjectTypeOf_ReadsTheTableTagAsWritten(t *testing.T) {
	type bare struct {
		bun.BaseModel `bun:"table:device"`
	}
	type qualified struct {
		bun.BaseModel `bun:"table:inventory.device,alias:d"`
	}
	if got, err := sdb.ObjectTypeOf(&bare{}); err != nil || got != "device" {
		t.Fatalf("expected the bare tag untouched, got %q, %v", got, err)
	}
	if got, err := sdb.ObjectTypeOf(qualified{}); err != nil || got != "inventory.device" {
		t.Fatalf("expected the qualified tag untouched, got %q, %v", got, err)
	}
}
