package shared_test

import (
	"testing"

	"github.com/google/uuid"
	sdb "github.com/nimling/samna-auth-middleware/shared"
)

func TestStampAccessClaim_StampsPointerAndValueCreators(t *testing.T) {
	userClaimID := uuid.New()
	ac := &sdb.AccessClaim{UserClaimID: &userClaimID, OwnerID: uuid.New()}

	claim := &sdb.Claim{}
	sdb.StampAccessClaim(claim, ac)
	if claim.CreatedBy == nil || *claim.CreatedBy != userClaimID {
		t.Fatalf("claim created_by: want %s, got %v", userClaimID, claim.CreatedBy)
	}

	binding := &sdb.ClaimObject{}
	sdb.StampAccessClaim(binding, ac)
	if binding.CreatedBy == nil || *binding.CreatedBy != userClaimID {
		t.Fatalf("claim_object created_by: want %s, got %v", userClaimID, binding.CreatedBy)
	}

	secret := &sdb.SamnaSecret{}
	sdb.StampAccessClaim(secret, ac)
	if secret.CreatedBy == nil || *secret.CreatedBy != userClaimID {
		t.Fatalf("samna_secret created_by: want %s, got %v", userClaimID, secret.CreatedBy)
	}

	org := &sdb.Organization{}
	sdb.StampAccessClaim(org, ac)
	if org.CreatedBy != userClaimID {
		t.Fatalf("organization created_by: want %s, got %s", userClaimID, org.CreatedBy)
	}
}

func TestStampAccessClaim_KeepsAnExplicitCreator(t *testing.T) {
	userClaimID := uuid.New()
	explicit := uuid.New()
	ac := &sdb.AccessClaim{UserClaimID: &userClaimID}

	claim := &sdb.Claim{CreatedBy: &explicit}
	sdb.StampAccessClaim(claim, ac)
	if *claim.CreatedBy != explicit {
		t.Fatalf("claim created_by: want the explicit %s, got %s", explicit, *claim.CreatedBy)
	}
}
