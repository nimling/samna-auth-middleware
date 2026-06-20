package claimius

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

//go:embed database/shared database/prophet database/disciple
var SQL embed.FS

var roles = map[string]string{
	"prophet":  "database/prophet",
	"disciple": "database/disciple",
}

func WriteRole(role, outDir string) error {
	roleDir, ok := roles[role]
	if !ok {
		return fmt.Errorf("unknown role %q: want prophet or disciple", role)
	}
	if err := writeTree("database/shared", filepath.Join(outDir, "shared")); err != nil {
		return err
	}
	if err := writeTree(roleDir, filepath.Join(outDir, "main")); err != nil {
		return err
	}
	return nil
}

func writeTree(srcDir, dstDir string) error {
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return err
	}
	entries, err := fs.ReadDir(SQL, srcDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := SQL.ReadFile(filepath.Join(srcDir, entry.Name()))
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dstDir, entry.Name()), data, 0o644); err != nil {
			return err
		}
	}
	return nil
}
