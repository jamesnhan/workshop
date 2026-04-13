// Package testhelpers provides shared fixtures for backend tests.
//
// Conventions:
//   - Every helper takes *testing.T (or testing.TB) and registers cleanup
//     via t.Cleanup so tests don't need to remember to tear down.
//   - Nothing here should reach over the network, touch the user's real
//     filesystem outside of t.TempDir(), or leave state behind after a
//     test exits.
package testhelpers

import (
	"testing"

	"github.com/jamesnhan/workshop/internal/db"
)

// TempDataDir returns a per-test temporary directory. Cleanup is automatic.
func TempDataDir(t testing.TB) string {
	t.Helper()
	return t.TempDir()
}

// TempDB opens a fresh SQLite database rooted in a per-test tempdir and
// registers cleanup. Use for any test that exercises db.DB methods.
func TempDB(t testing.TB) *db.DB {
	t.Helper()
	dir := TempDataDir(t)
	d, err := db.Open(dir)
	if err != nil {
		t.Fatalf("open temp db: %v", err)
	}
	t.Cleanup(func() {
		_ = d.Close()
	})
	return d
}
