package testhelpers

import (
	"os/exec"
	"testing"
)

// GitRepo wraps a temporary git repository that tests can drive via helper
// methods. Cleanup is automatic via t.TempDir.
type GitRepo struct {
	t   testing.TB
	Dir string
}

// NewGitRepo initializes a fresh git repo in a per-test tempdir, configures
// a dummy author so commits succeed, and returns a wrapper with helpers for
// common operations.
func NewGitRepo(t testing.TB) *GitRepo {
	t.Helper()
	dir := TempDataDir(t)
	r := &GitRepo{t: t, Dir: dir}
	r.Run("init", "-b", "main")
	r.Run("config", "user.email", "test@workshop.local")
	r.Run("config", "user.name", "Workshop Test")
	// Disable commit signing since CI / sandboxes often lack a signing key.
	r.Run("config", "commit.gpgsign", "false")
	return r
}

// Run invokes git with the given args in the repo directory. Fails the test
// on non-zero exit.
func (r *GitRepo) Run(args ...string) string {
	r.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = r.Dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
	return string(out)
}

// TryRun is like Run but doesn't fail the test on error — use when the
// call is allowed to fail (e.g. optional setup).
func (r *GitRepo) TryRun(args ...string) (string, error) {
	r.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = r.Dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// WriteFile writes a file relative to the repo root.
func (r *GitRepo) WriteFile(rel, content string) {
	r.t.Helper()
	if err := writeFile(r.Dir, rel, content); err != nil {
		r.t.Fatalf("write file %s: %v", rel, err)
	}
}

// Commit stages everything and creates a commit with the given message.
func (r *GitRepo) Commit(message string) {
	r.t.Helper()
	r.Run("add", "-A")
	r.Run("commit", "-m", message, "--allow-empty")
}
