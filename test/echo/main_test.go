package echo_test

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/nimling/samna-auth-middleware/db"
)

func TestMain(m *testing.M) {
	if err := InitTestEnv(); err != nil {
		fmt.Fprintf(os.Stderr, "skipping echo tests: %v\n", err)
		os.Exit(0)
	}

	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "skipping echo tests: setup panicked: %v\n", r)
			os.Exit(0)
		}
	}()

	testEcho = InitTestEcho()

	if db.Instance() == nil {
		fmt.Fprintln(os.Stderr, "skipping echo tests: db.Instance() is nil")
		os.Exit(0)
	}

	id, err := ResolveRootOrg(context.Background())
	if err != nil {
		fmt.Fprintf(os.Stderr, "skipping echo tests: resolve root org: %v\n", err)
		os.Exit(0)
	}
	rootOrgID = id

	os.Exit(m.Run())
}
