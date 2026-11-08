//go:build !dbtest

package sam_test

import (
	"errors"
	"testing"

	sam "github.com/nimling/samna-auth-middleware/v2/disciple/sam"
	"github.com/nimling/samna-auth-middleware/v2/shared"
)

func TestInitWithoutDatabaseIsRefused(t *testing.T) {
	if err := sam.Init(t.Context()); !errors.Is(err, shared.ErrNotConnected) {
		t.Fatalf("init = %v, want ErrNotConnected", err)
	}
	if err := sam.PeerInit(t.Context()); !errors.Is(err, shared.ErrNotConnected) {
		t.Fatalf("peer init = %v, want ErrNotConnected", err)
	}
}
