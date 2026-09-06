package cli

import (
	"strings"
	"testing"
)

func TestFolderDuplicateName(t *testing.T) {
	t.Chdir(t.TempDir())

	mustRun(t, exitSuccess, "init")
	mustRun(t, exitSuccess, "collection", "create", "API")
	mustRun(t, exitSuccess, "folder", "create", "API/Auth")

	_, stderr := mustRun(t, exitUsage, "folder", "create", "API/Auth")
	if !strings.Contains(stderr, "already exists") || !strings.Contains(stderr, "folder") {
		t.Errorf("duplicate folder: stderr = %q, want the collision named as a folder", stderr)
	}

	_, stderr = mustRun(t, exitUsage, "request", "create", "API/Auth", "--method", "GET", "--url", "http://example.com/")
	if !strings.Contains(stderr, "already exists") {
		t.Errorf("request colliding with a folder: stderr = %q, want the collision named", stderr)
	}
}

func TestFolderParents(t *testing.T) {
	t.Chdir(t.TempDir())

	mustRun(t, exitSuccess, "init")
	mustRun(t, exitSuccess, "collection", "create", "API")

	_, stderr := mustRun(t, exitUsage, "folder", "create", "API/A/B")
	if !strings.Contains(stderr, `no folder named "A"`) {
		t.Errorf("missing parent: stderr = %q, want the missing folder named", stderr)
	}

	mustRun(t, exitSuccess, "folder", "create", "API/A/B", "--parents")

	stdout, _ := mustRun(t, exitSuccess, "tree", "API")
	if !strings.Contains(stdout, "A/") || !strings.Contains(stdout, "B/") {
		t.Errorf("tree = %q, want it to contain A/ and B/", stdout)
	}
}
