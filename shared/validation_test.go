package shared_test

import (
	"testing"

	sdb "github.com/nimling/samna-auth-middleware/v2/shared"
)

func TestOrganizationValidatePutAcceptsEmptyType(t *testing.T) {
	org := sdb.Organization{Name: "Acme"}
	if err := org.ValidatePut(); err != nil {
		t.Fatalf("expected an empty type to be accepted on update, got %v", err)
	}
}

func TestOrganizationValidatePutRejectsSystemType(t *testing.T) {
	org := sdb.Organization{Name: "Acme", Type: sdb.ORGANIZATION_TYPE_SYSTEM}
	if err := org.ValidatePut(); err == nil {
		t.Fatal("expected the system type to be rejected on update")
	}
}
