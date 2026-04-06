package tmux

import (
	"os/exec"
	"testing"
)

// fakeCmd returns a CommandRunner that records invocations and returns fixed output.
func fakeCmd(output string, exitCode int) (CommandRunner, *[][]string) {
	var calls [][]string
	runner := func(name string, args ...string) *exec.Cmd {
		calls = append(calls, append([]string{name}, args...))
		if exitCode != 0 {
			cmd := exec.Command("sh", "-c", "echo '"+output+"' >&2; exit "+string(rune('0'+exitCode)))
			return cmd
		}
		cmd := exec.Command("echo", "-n", output)
		return cmd
	}
	return runner, &calls
}

func TestListSessions(t *testing.T) {
	output := "mysession\t3\t1711900000\t1\nother\t1\t1711900100\t0"
	runner, _ := fakeCmd(output, 0)

	b := &ExecBridge{tmuxPath: "tmux", runCmd: runner}
	sessions, err := b.ListSessions()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(sessions))
	}
	if sessions[0].Name != "mysession" {
		t.Errorf("expected name 'mysession', got %q", sessions[0].Name)
	}
	if sessions[0].Windows != 3 {
		t.Errorf("expected 3 windows, got %d", sessions[0].Windows)
	}
	if !sessions[0].Attached {
		t.Error("expected session to be attached")
	}
	if sessions[1].Attached {
		t.Error("expected second session to not be attached")
	}
}

func TestListSessionsNoServer(t *testing.T) {
	runner, _ := fakeCmd("no server running on /tmp/tmux-1000/default", 1)

	b := &ExecBridge{tmuxPath: "tmux", runCmd: runner}
	sessions, err := b.ListSessions()
	if err != nil {
		t.Fatalf("expected nil error for no server, got: %v", err)
	}
	if len(sessions) != 0 {
		t.Fatalf("expected 0 sessions, got %d", len(sessions))
	}
}
