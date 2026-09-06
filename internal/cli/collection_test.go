package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// mustRun executes one command and fails the test unless it exited want.
func mustRun(t *testing.T, want int, args ...string) (string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), args, &stdout, &stderr)
	if code != want {
		t.Fatalf("%v: exit = %d, want %d (stdout: %q, stderr: %q)",
			args, code, want, stdout.String(), stderr.String())
	}
	return stdout.String(), stderr.String()
}

// createdID extracts the id from a `created collection "NAME" (<id>)` line.
func createdID(t *testing.T, stderr string) string {
	t.Helper()
	open := strings.LastIndex(stderr, "(")
	end := strings.LastIndex(stderr, ")")
	if open < 0 || end < open {
		t.Fatalf("no id in %q", stderr)
	}
	return stderr[open+1 : end]
}

func TestCollectionCreateList(t *testing.T) {
	t.Chdir(t.TempDir())

	mustRun(t, exitSuccess, "init")
	_, stderr := mustRun(t, exitSuccess, "collection", "create", "Zeta")
	zetaID := createdID(t, stderr)
	_, stderr = mustRun(t, exitSuccess, "collection", "create", "Alpha")
	alphaID := createdID(t, stderr)

	_, stderr = mustRun(t, exitUsage, "collection", "create", "Zeta")
	if !strings.Contains(stderr, "already exists") {
		t.Errorf("duplicate create: stderr = %q, want a collision mention", stderr)
	}

	stdout := ""
	stdout, _ = mustRun(t, exitSuccess, "collection", "list")
	want := "Alpha\t" + alphaID + "\nZeta\t" + zetaID + "\n"
	if stdout != want {
		t.Errorf("list = %q, want %q", stdout, want)
	}

	// The global --workspace form resolves the same workspace.
	stdout, _ = mustRun(t, exitSuccess, "--workspace", ".", "collection", "list")
	if stdout != want {
		t.Errorf("--workspace list = %q, want %q", stdout, want)
	}

	mustRun(t, exitUsage, "collection", "create")
	mustRun(t, exitUsage, "collection", "create", "X", "Y")
	mustRun(t, exitUsage, "collection", "create", "X", "--bogus")
	mustRun(t, exitUsage, "collection")
	mustRun(t, exitUsage, "collection", "rename", "A", "B") // missing OLD: covered by TestRenameCommands
	mustRun(t, exitUsage, "collection", "list", "extra")
}

func TestWorkspaceMissingForStoreCommands(t *testing.T) {
	t.Chdir(t.TempDir()) // no .req here or in any ancestor

	_, stderr := mustRun(t, exitUsage, "collection", "list")
	if !strings.Contains(stderr, "req init") || !strings.Contains(stderr, "--workspace") {
		t.Errorf("collection list: stderr = %q, want a hint naming `req init` and --workspace", stderr)
	}

	_, stderr = mustRun(t, exitUsage, "run", "API/Ping")
	if !strings.Contains(stderr, "req init") || !strings.Contains(stderr, "--workspace") {
		t.Errorf("run: stderr = %q, want a hint naming `req init` and --workspace", stderr)
	}
}
