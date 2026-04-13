package testhelpers

import (
	"os"
	"path/filepath"
)

// writeFile writes `content` to `rel` inside `dir`, creating any parent
// directories. Used by GitRepo.WriteFile.
func writeFile(dir, rel, content string) error {
	full := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	return os.WriteFile(full, []byte(content), 0o644)
}
