package prophet

import (
	"embed"
	"path/filepath"

	"github.com/nimling/samna-auth-middleware/shared"
)

//go:embed database
var SQL embed.FS

func WriteSQL(outDir string) error {
	if err := shared.WriteTree(shared.SQL, "database", filepath.Join(outDir, "shared")); err != nil {
		return err
	}
	return shared.WriteTree(SQL, "database", filepath.Join(outDir, "main"))
}
