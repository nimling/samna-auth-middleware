package claimius

import (
	"fmt"

	"github.com/nimling/samna-auth-middleware/disciple"
	"github.com/nimling/samna-auth-middleware/prophet"
)

func WriteRole(role, outDir string) error {
	switch role {
	case "prophet":
		return prophet.WriteSQL(outDir)
	case "disciple":
		return disciple.WriteSQL(outDir)
	default:
		return fmt.Errorf("unknown role %q: want prophet or disciple", role)
	}
}
