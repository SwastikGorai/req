package cli

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// overrideStdinIsTerminal replaces stdinIsTerminal for the duration of the
// test.
func overrideStdinIsTerminal(t *testing.T, fake func() bool) {
	t.Helper()
	orig := stdinIsTerminal
	stdinIsTerminal = fake
	t.Cleanup(func() { stdinIsTerminal = orig })
}

// overrideConfirmDelete replaces confirmDelete for the duration of the test.
func overrideConfirmDelete(t *testing.T, fake func(string, io.Writer) bool) {
	t.Helper()
	orig := confirmDelete
	confirmDelete = fake
	t.Cleanup(func() { confirmDelete = orig })
}

// allCollectionBytes returns the concatenated bytes of every stored
// collection file, for no-op assertions in workspaces with several
// collections (collectionFileBytes requires exactly one).
func allCollectionBytes(t *testing.T) []byte {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(".req", "collections", "*.json"))
	if err != nil || len(matches) == 0 {
		t.Fatalf("globbing collections: %v (%d matches)", err, len(matches))
	}
	sort.Strings(matches)
	var buf bytes.Buffer
	for _, m := range matches {
		data, err := os.ReadFile(m)
		if err != nil {
			t.Fatalf("reading %s: %v", m, err)
		}
		buf.Write(data)
	}
	return buf.Bytes()
}

func TestRenameCommands(t *testing.T) {
	t.Chdir(t.TempDir())

	mustRun(t, exitSuccess, "init")
	mustRun(t, exitSuccess, "collection", "create", "Zeta")
	mustRun(t, exitSuccess, "collection", "create", "API")
	mustRun(t, exitSuccess, "folder", "create", "API/Auth")
	mustRun(t, exitSuccess, "request", "create", "API/Auth/Login", "--method", "GET", "--url", "https://x/login")

	// Collection rename: the old name stops resolving, the new one works.
	_, stderr := mustRun(t, exitSuccess, "collection", "rename", "Zeta", "Alpha")
	if !strings.Contains(stderr, `renamed collection "Zeta" to "Alpha"`) {
		t.Errorf("rename stderr = %q, want the rename confirmation", stderr)
	}
	stdout, _ := mustRun(t, exitSuccess, "collection", "list")
	if !strings.Contains(stdout, "Alpha\t") || strings.Contains(stdout, "Zeta") {
		t.Errorf("list = %q, want Alpha and no Zeta", stdout)
	}
	_, stderr = mustRun(t, exitUsage, "collection", "rename", "Zeta", "Other")
	if !strings.Contains(stderr, "not found") {
		t.Errorf("renaming a missing collection: stderr = %q, want not found", stderr)
	}
	_, stderr = mustRun(t, exitUsage, "collection", "rename", "API", "Alpha")
	if !strings.Contains(stderr, "already exists") {
		t.Errorf("renaming onto an existing collection: stderr = %q, want the collision named", stderr)
	}
	_, stderr = mustRun(t, exitUsage, "collection", "rename", "API", "a/b")
	if !strings.Contains(stderr, "invalid") {
		t.Errorf("renaming to an invalid name: stderr = %q, want the naming rule", stderr)
	}
	mustRun(t, exitUsage, "collection", "rename", "API")
	mustRun(t, exitUsage, "collection", "rename", "API", "X", "Y")
	mustRun(t, exitUsage, "collection", "rename", "API", "X", "--bogus")

	// Folder rename: the tree shows the new name, the old path is gone.
	mustRun(t, exitSuccess, "folder", "rename", "API/Auth", "Session")
	stdout, _ = mustRun(t, exitSuccess, "tree", "API")
	if strings.Contains(stdout, "Auth") || !strings.Contains(stdout, "Session/") {
		t.Errorf("tree = %q, want Session/ and no Auth", stdout)
	}
	mustRun(t, exitUsage, "folder", "rename", "API/Nope", "X")
	mustRun(t, exitSuccess, "folder", "create", "API/Tools")
	_, stderr = mustRun(t, exitUsage, "folder", "rename", "API/Session", "Tools")
	if !strings.Contains(stderr, "already exists") {
		t.Errorf("renaming onto a sibling name: stderr = %q, want the collision named", stderr)
	}
	_, stderr = mustRun(t, exitUsage, "folder", "rename", "API/Session", "a/b")
	if !strings.Contains(stderr, "invalid") {
		t.Errorf("renaming to an invalid name: stderr = %q, want the naming rule", stderr)
	}
	before := allCollectionBytes(t)
	mustRun(t, exitSuccess, "folder", "rename", "API/Session", "Session")
	if after := allCollectionBytes(t); !bytes.Equal(before, after) {
		t.Error("same-name folder rename rewrote the collection file")
	}

	// Request rename: list shows the new name at the same place.
	mustRun(t, exitSuccess, "request", "rename", "API/Session/Login", "Signin")
	stdout, _ = mustRun(t, exitSuccess, "request", "list", "API/Session")
	if stdout != "Signin\tGET\thttps://x/login\n" {
		t.Errorf("list = %q, want the renamed request", stdout)
	}
	mustRun(t, exitSuccess, "request", "create", "API/Session/Other", "--method", "GET", "--url", "https://x/other")
	_, stderr = mustRun(t, exitUsage, "request", "rename", "API/Session/Signin", "Other")
	if !strings.Contains(stderr, "already exists") {
		t.Errorf("renaming onto a sibling request: stderr = %q, want the collision named", stderr)
	}
	before = allCollectionBytes(t)
	mustRun(t, exitSuccess, "request", "rename", "API/Session/Signin", "Signin")
	if after := allCollectionBytes(t); !bytes.Equal(before, after) {
		t.Error("same-name request rename rewrote the collection file")
	}
}

func TestMoveCommands(t *testing.T) {
	t.Chdir(t.TempDir())

	mustRun(t, exitSuccess, "init")
	mustRun(t, exitSuccess, "collection", "create", "API")
	mustRun(t, exitSuccess, "folder", "create", "API/Auth")
	mustRun(t, exitSuccess, "folder", "create", "API/Tools")
	mustRun(t, exitSuccess, "request", "create", "API/Auth/Login", "--method", "GET", "--url", "https://x/login")
	mustRun(t, exitSuccess, "request", "create", "API/Ping", "--method", "GET", "--url", "https://x/ping")

	// A request move: last child at the destination, gone from the old
	// parent.
	mustRun(t, exitSuccess, "request", "move", "API/Auth/Login", "API/Tools")
	stdout, _ := mustRun(t, exitSuccess, "request", "list", "API/Auth")
	if stdout != "" {
		t.Errorf("old parent list = %q, want empty", stdout)
	}
	stdout, _ = mustRun(t, exitSuccess, "request", "list", "API/Tools")
	if stdout != "Login\tGET\thttps://x/login\n" {
		t.Errorf("destination list = %q, want the moved Login request", stdout)
	}

	// A folder move takes the whole subtree along.
	mustRun(t, exitSuccess, "folder", "create", "API/Auth/Deep", "--parents")
	mustRun(t, exitSuccess, "request", "create", "API/Auth/Deep/Init", "--method", "POST", "--url", "https://x/init")
	mustRun(t, exitSuccess, "folder", "move", "API/Auth/Deep", "API/Tools")
	stdout, _ = mustRun(t, exitSuccess, "tree", "API/Tools")
	if want := "Tools\nLogin  GET https://x/login\nDeep/\n  Init  POST https://x/init\n"; stdout != want {
		t.Errorf("tree API/Tools = %q, want %q", stdout, want)
	}
	stdout, _ = mustRun(t, exitSuccess, "tree", "API/Auth")
	if stdout != "Auth\n" {
		t.Errorf("tree API/Auth = %q, want only the empty Auth root", stdout)
	}

	// A move under the current parent is a no-op that writes nothing.
	before := allCollectionBytes(t)
	mustRun(t, exitSuccess, "request", "move", "API/Tools/Login", "API/Tools")
	mustRun(t, exitSuccess, "folder", "move", "API/Tools", "API")
	if after := allCollectionBytes(t); !bytes.Equal(before, after) {
		t.Error("a same-parent move rewrote the collection file")
	}

	// Cross-collection moves are usage errors.
	mustRun(t, exitSuccess, "collection", "create", "Other")
	_, stderr := mustRun(t, exitUsage, "folder", "move", "API/Tools/Deep", "Other")
	if !strings.Contains(stderr, "another collection") {
		t.Errorf("cross-collection move: stderr = %q, want the other collection named", stderr)
	}
	// Moving under a request is rejected.
	_, stderr = mustRun(t, exitUsage, "folder", "move", "API/Tools/Deep", "API/Tools/Login")
	if !strings.Contains(stderr, "invalid move") {
		t.Errorf("move under a request: stderr = %q, want an invalid-move diagnostic", stderr)
	}
	// A missing destination is a usage error.
	_, stderr = mustRun(t, exitUsage, "folder", "move", "API/Tools/Deep", "API/Nope")
	if !strings.Contains(stderr, "not found") {
		t.Errorf("move to a missing destination: stderr = %q, want not found", stderr)
	}
	// Usage errors.
	mustRun(t, exitUsage, "folder", "move", "API/Tools")
	mustRun(t, exitUsage, "folder", "move", "API/Tools", "API", "extra")
	mustRun(t, exitUsage, "folder", "move", "API/Tools", "API", "--bogus")
}

func TestMoveCycleRejected(t *testing.T) {
	t.Chdir(t.TempDir())

	mustRun(t, exitSuccess, "init")
	mustRun(t, exitSuccess, "collection", "create", "API")
	mustRun(t, exitSuccess, "folder", "create", "API/A/B", "--parents")
	before := collectionFileBytes(t)

	_, stderr := mustRun(t, exitUsage, "folder", "move", "API/A", "API/A/B")
	if !strings.Contains(stderr, "itself") || !strings.Contains(stderr, "descendant") {
		t.Errorf("move into a descendant: stderr = %q, want itself/descendant named", stderr)
	}
	_, stderr = mustRun(t, exitUsage, "folder", "move", "API/A", "API/A")
	if !strings.Contains(stderr, "itself") {
		t.Errorf("move into itself: stderr = %q, want itself named", stderr)
	}
	if after := collectionFileBytes(t); !bytes.Equal(before, after) {
		t.Error("a rejected cycle move rewrote the collection file")
	}
	stdout, _ := mustRun(t, exitSuccess, "tree", "API")
	if want := "API\nA/\n  B/\n"; stdout != want {
		t.Errorf("tree = %q, want %q (unchanged)", stdout, want)
	}
}

func TestDeleteNoninteractive(t *testing.T) {
	t.Chdir(t.TempDir())

	mustRun(t, exitSuccess, "init")
	mustRun(t, exitSuccess, "collection", "create", "API")
	mustRun(t, exitSuccess, "folder", "create", "API/Auth")
	mustRun(t, exitSuccess, "request", "create", "API/Auth/Login", "--method", "GET", "--url", "https://x/login")

	overrideStdinIsTerminal(t, func() bool { return false })

	// Without --yes a nonempty folder delete is refused and deletes nothing.
	_, stderr := mustRun(t, exitUsage, "folder", "delete", "API/Auth")
	if !strings.Contains(stderr, "--yes") {
		t.Errorf("refusal stderr = %q, want it to point at --yes", stderr)
	}
	stdout, _ := mustRun(t, exitSuccess, "tree", "API")
	if !strings.Contains(stdout, "Auth/") {
		t.Errorf("tree = %q, want Auth/ still present", stdout)
	}

	// --yes deletes without asking.
	mustRun(t, exitSuccess, "folder", "delete", "API/Auth", "--yes")
	stdout, _ = mustRun(t, exitSuccess, "tree", "API")
	if strings.Contains(stdout, "Auth") {
		t.Errorf("tree = %q, want Auth gone", stdout)
	}

	// A nonempty collection needs the same gate.
	mustRun(t, exitSuccess, "request", "create", "API/Ping", "--method", "GET", "--url", "https://x/ping")
	_, stderr = mustRun(t, exitUsage, "collection", "delete", "API")
	if !strings.Contains(stderr, "--yes") {
		t.Errorf("collection refusal stderr = %q, want it to point at --yes", stderr)
	}
	stdout, _ = mustRun(t, exitSuccess, "collection", "list")
	if !strings.Contains(stdout, "API\t") {
		t.Errorf("list = %q, want API still listed", stdout)
	}

	// An interactive user who declines keeps everything and exits 0.
	overrideStdinIsTerminal(t, func() bool { return true })
	var prompt string
	overrideConfirmDelete(t, func(p string, _ io.Writer) bool {
		prompt = p
		return false
	})
	mustRun(t, exitSuccess, "folder", "create", "API/Auth")
	mustRun(t, exitSuccess, "request", "create", "API/Auth/Login", "--method", "GET", "--url", "https://x/login")
	_, stderr = mustRun(t, exitSuccess, "folder", "delete", "API/Auth")
	if !strings.Contains(stderr, "aborted") {
		t.Errorf("declined delete stderr = %q, want the abort notice", stderr)
	}
	if !strings.Contains(prompt, "API/Auth") || !strings.Contains(prompt, "1 item") {
		t.Errorf("prompt = %q, want it to name the folder and its item count", prompt)
	}
	stdout, _ = mustRun(t, exitSuccess, "tree", "API")
	if !strings.Contains(stdout, "Auth/") {
		t.Errorf("tree = %q, want Auth/ kept after the declined prompt", stdout)
	}

	// An explicit confirmation deletes.
	overrideConfirmDelete(t, func(string, io.Writer) bool { return true })
	mustRun(t, exitSuccess, "folder", "delete", "API/Auth")
	stdout, _ = mustRun(t, exitSuccess, "tree", "API")
	if strings.Contains(stdout, "Auth") {
		t.Errorf("tree = %q, want Auth gone after the confirmed prompt", stdout)
	}
}

func TestDeleteCommands(t *testing.T) {
	t.Chdir(t.TempDir())

	mustRun(t, exitSuccess, "init")
	mustRun(t, exitSuccess, "collection", "create", "API")
	mustRun(t, exitSuccess, "folder", "create", "API/Auth")

	// An empty folder deletes without any confirmation: even with stdin
	// not a terminal, an empty target must not trigger the gate.
	overrideStdinIsTerminal(t, func() bool { return false })
	mustRun(t, exitSuccess, "folder", "delete", "API/Auth")
	stdout, _ := mustRun(t, exitSuccess, "tree", "API")
	if strings.Contains(stdout, "Auth") {
		t.Errorf("tree = %q, want Auth gone", stdout)
	}

	// A request deletes without --yes; --yes is accepted for symmetry.
	mustRun(t, exitSuccess, "request", "create", "API/Ping", "--method", "GET", "--url", "https://x/ping")
	mustRun(t, exitSuccess, "request", "create", "API/Pong", "--method", "GET", "--url", "https://x/pong")
	mustRun(t, exitSuccess, "request", "delete", "API/Ping")
	stdout, _ = mustRun(t, exitSuccess, "request", "list", "API")
	if stdout != "Pong\tGET\thttps://x/pong\n" {
		t.Errorf("list = %q, want only Pong", stdout)
	}
	mustRun(t, exitSuccess, "request", "delete", "API/Pong", "--yes")
	stdout, _ = mustRun(t, exitSuccess, "request", "list", "API")
	if stdout != "" {
		t.Errorf("list = %q, want empty", stdout)
	}

	// Wrong-kind deletes point at the command that owns the kind.
	mustRun(t, exitSuccess, "folder", "create", "API/F")
	mustRun(t, exitSuccess, "request", "create", "API/R", "--method", "GET", "--url", "https://x/r")
	_, stderr := mustRun(t, exitUsage, "folder", "delete", "API/R")
	if !strings.Contains(stderr, "request delete") {
		t.Errorf("folder delete of a request: stderr = %q, want a pointer to request delete", stderr)
	}
	_, stderr = mustRun(t, exitUsage, "request", "delete", "API/F")
	if !strings.Contains(stderr, "folder delete") {
		t.Errorf("request delete of a folder: stderr = %q, want a pointer to folder delete", stderr)
	}
	_, stderr = mustRun(t, exitUsage, "folder", "delete", "API")
	if !strings.Contains(stderr, "collection delete") {
		t.Errorf("folder delete of the collection root: stderr = %q, want a pointer to collection delete", stderr)
	}
	_, stderr = mustRun(t, exitUsage, "request", "delete", "API")
	if !strings.Contains(stderr, "collection delete") {
		t.Errorf("request delete of the collection root: stderr = %q, want a pointer to collection delete", stderr)
	}
	_, stderr = mustRun(t, exitUsage, "collection", "delete", "API/F")
	if !strings.Contains(stderr, "an item, not a collection") {
		t.Errorf("collection delete of an item path: stderr = %q, want the item named", stderr)
	}

	// An empty collection deletes without --yes.
	mustRun(t, exitSuccess, "collection", "create", "Empty")
	mustRun(t, exitSuccess, "collection", "delete", "Empty")
	stdout, _ = mustRun(t, exitSuccess, "collection", "list")
	if strings.Contains(stdout, "Empty") {
		t.Errorf("list = %q, want Empty gone", stdout)
	}

	// Usage errors.
	mustRun(t, exitUsage, "folder", "delete")
	mustRun(t, exitUsage, "request", "delete")
	mustRun(t, exitUsage, "folder", "delete", "API/F", "--bogus")
	mustRun(t, exitUsage, "request", "delete", "API/R", "--bogus")
	mustRun(t, exitUsage, "request", "delete", "API/Nope")
}
