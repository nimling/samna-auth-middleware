package shared

import (
	"embed"
	"io/fs"
	"os"
	"path"
	"path/filepath"
)

//go:embed database
var SQL embed.FS

func WriteTree(fsys fs.FS, srcDir, dstDir string) error {
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return err
	}
	entries, err := fs.ReadDir(fsys, srcDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := fs.ReadFile(fsys, path.Join(srcDir, entry.Name()))
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dstDir, entry.Name()), data, 0o644); err != nil {
			return err
		}
	}
	return nil
}
