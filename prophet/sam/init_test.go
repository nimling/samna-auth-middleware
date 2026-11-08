//go:build !dbtest

package sam_test

import (
	"errors"
	"testing"

	sam "github.com/nimling/samna-auth-middleware/v2/prophet/sam"
	"github.com/nimling/samna-auth-middleware/v2/shared"
)

func TestInitWithoutDatabaseIsRefused(t *testing.T) {
	if err := sam.Init(t.Context()); !errors.Is(err, shared.ErrNotConnected) {
		t.Fatalf("init = %v, want ErrNotConnected", err)
	}
	select {
	case <-sam.Done():
	default:
		t.Fatalf("Done before Init blocks")
	}
}
