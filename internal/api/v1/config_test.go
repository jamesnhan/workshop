package v1

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsAllowedConfigPath_ValidPath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	valid := filepath.Join(home, ".config", "workshop", "init.lua")
	assert.True(t, isAllowedConfigPath(valid))
}

func TestIsAllowedConfigPath_ValidSubdir(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	valid := filepath.Join(home, ".config", "workshop", "plugins", "custom.lua")
	assert.True(t, isAllowedConfigPath(valid))
}

func TestIsAllowedConfigPath_OutsideConfigDir(t *testing.T) {
	cases := []string{
		"/etc/passwd",
		"/tmp/evil.lua",
		"/root/.config/workshop/init.lua",
	}
	for _, p := range cases {
		assert.False(t, isAllowedConfigPath(p), "should reject %s", p)
	}
}

func TestIsAllowedConfigPath_TraversalAttack(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	traversal := filepath.Join(home, ".config", "workshop", "..", "..", "..", "etc", "passwd")
	assert.False(t, isAllowedConfigPath(traversal))
}

func TestIsAllowedConfigPath_SiblingDir(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	// ~/.config/evil — sibling of workshop, not inside it.
	sibling := filepath.Join(home, ".config", "evil", "init.lua")
	assert.False(t, isAllowedConfigPath(sibling))
}

func TestIsAllowedConfigPath_EmptyPath(t *testing.T) {
	assert.False(t, isAllowedConfigPath(""))
}

func TestIsAllowedConfigPath_RelativePath(t *testing.T) {
	// Relative paths resolve to cwd, which shouldn't be under ~/.config/workshop.
	assert.False(t, isAllowedConfigPath("init.lua"))
}

func TestIsAllowedConfigPath_SymlinkEscape(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}

	configDir := filepath.Join(home, ".config", "workshop")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Skip("cannot create config dir")
	}

	// Create a symlink inside ~/.config/workshop that points outside.
	link := filepath.Join(configDir, "workshop-test-escape-link.lua")
	_ = os.Remove(link)
	if err := os.Symlink("/etc/hosts", link); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}
	defer os.Remove(link)

	// The symlink resolves to /etc/hosts which is outside ~/.config/workshop.
	assert.False(t, isAllowedConfigPath(link))
}

func TestIsAllowedConfigPath_EmptyHOME(t *testing.T) {
	// Temporarily unset HOME to test the guard.
	orig := os.Getenv("HOME")
	t.Setenv("HOME", "")
	defer os.Setenv("HOME", orig)

	assert.False(t, isAllowedConfigPath("/anything"))
}
