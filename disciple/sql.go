package disciple

import (
	"embed"

	"github.com/nimling/samna-auth-middleware/shared"
)

//go:embed database
var SQL embed.FS

func WriteSQL(outDir string) error {
	return shared.WriteTree(SQL, "database", outDir)
}
