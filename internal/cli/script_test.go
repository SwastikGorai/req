package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"req/internal/model"
	"req/internal/store"
)

// setScripts stores scripts on the node at path, for tests that prepare
// script state without driving the edit command.
func setScripts(t *testing.T, path string, s *model.Scripts) {
	t.Helper()
	ws, err := store.Open("")
	if err != nil {
		t.Fatalf("opening workspace: %v", err)
	}
	ctx := context.Background()
	rp, err := ws.ResolvePath(ctx, path)
	if err != nil {
		t.Fatalf("resolving %s: %v", path, err)
	}
	if err := ws.UpdateScripts(ctx, path, s, rp.Rev); err != nil {
		t.Fatalf("UpdateScripts %s: %v", path, err)
	}
}

// storedRequest loads the saved request at path from the workspace on disk.
func storedRequest(t *testing.T, path string) model.Request {
	t.Helper()
	ws, err := store.Open("")
	if err != nil {
		t.Fatalf("opening workspace: %v", err)
	}
	rp, err := ws.ResolvePath(context.Background(), path)
	if err != nil {
		t.Fatalf("resolving %s: %v", path, err)
	}
	return *rp.Item.Request
}

// leftoverScriptEdits lists req-script temp files still present in the OS
// temp directory.
func leftoverScriptEdits(t *testing.T) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(os.TempDir(), "req-script-*.js"))
	if err != nil {
		t.Fatalf("globbing temp script edits: %v", err)
	}
	return matches
}

func TestScriptEditHappyPath(t *testing.T) {
	t.Chdir(t.TempDir())
	editSetup(t)

	t.Setenv("EDITOR", "fake-editor")
	t.Setenv("VISUAL", "")

	edited := "console.log(\"hi\")\n"
	var gotArgv []string
	overrideRunEditor(t, func(argv []string) error {
		gotArgv = argv
		return os.WriteFile(argv[len(argv)-1], []byte(edited), 0o644)
	})

	_, stderr := mustRun(t, exitSuccess, "script", "edit", "API/Ping", "--pre")
	if !strings.Contains(stderr, `updated scripts "API/Ping"`) {
		t.Errorf("stderr = %q, want the update confirmation", stderr)
	}
	if len(gotArgv) != 2 {
		t.Fatalf("editor argv = %v, want [EDITOR tempfile]", gotArgv)
	}
	if base := filepath.Base(gotArgv[1]); !strings.HasPrefix(base, "req-script-") || !strings.HasSuffix(base, ".js") {
		t.Errorf("temp file = %q, want a req-script-*.js name", gotArgv[1])
	}

	stdout, _ := mustRun(t, exitSuccess, "request", "show", "API/Ping")
	var req model.Request
	if err := json.Unmarshal([]byte(stdout), &req); err != nil {
		t.Fatalf("show output is not JSON: %v\n%s", err, stdout)
	}
	if req.Scripts == nil || len(req.Scripts.PreRequest) != 1 {
		t.Fatalf("show scripts = %+v, want one pre_request entry", req.Scripts)
	}
	sc := req.Scripts.PreRequest[0]
	if !strings.HasPrefix(sc.ID, "script-") {
		t.Errorf("script id = %q, want the script- prefix", sc.ID)
	}
	if sc.Source != edited {
		t.Errorf("script source = %q, want %q", sc.Source, edited)
	}
	if !sc.Enabled {
		t.Error("the created entry must be enabled")
	}
	if left := leftoverScriptEdits(t); len(left) != 0 {
		t.Errorf("temp script edit files remain: %v", left)
		for _, p := range left {
			os.Remove(p)
		}
	}

	// Usage errors: exactly one PATH and exactly one of --pre/--post.
	mustRun(t, exitUsage, "script", "edit", "API/Ping", "--pre", "--post")
	mustRun(t, exitUsage, "script", "edit", "API/Ping")
	mustRun(t, exitUsage, "script", "edit", "API/Ping", "extra", "--pre")
	mustRun(t, exitUsage, "script", "edit", "API/Ping", "--bogus")
	mustRun(t, exitUsage, "script", "edit", "API/Nope", "--pre")
	mustRun(t, exitUsage, "script")
	mustRun(t, exitUsage, "script", "bogus", "API/Ping", "--pre")

	// A marker line inside brand-new content is refused instead of being
	// stored as source text that would break the next edit.
	overrideRunEditor(t, func(argv []string) error {
		return os.WriteFile(argv[len(argv)-1], []byte("console.log(\"x\")\n// ---- req script fake-id ----\n"), 0o644)
	})
	_, stderr = mustRun(t, exitUsage, "script", "edit", "API/Ping", "--post")
	if !strings.Contains(stderr, "unexpected script separator") {
		t.Errorf("stderr = %q, want the unexpected-separator diagnostic", stderr)
	}
}

func TestScriptEditRoundTrip(t *testing.T) {
	t.Chdir(t.TempDir())
	editSetup(t)

	setScripts(t, "API/Ping", &model.Scripts{
		PreRequest: []model.Script{
			{ID: "pre-1", Source: "one\n", Enabled: true},
			{ID: "pre-2", Source: "two\n", Enabled: true},
		},
		PostResponse: []model.Script{
			{ID: "post-1", Source: "posted\n", Enabled: true},
		},
	})

	t.Setenv("EDITOR", "fake-editor")
	overrideRunEditor(t, func(argv []string) error {
		original, err := os.ReadFile(argv[len(argv)-1])
		if err != nil {
			return err
		}
		// Modify only the second pre entry's source; markers survive.
		return os.WriteFile(argv[len(argv)-1],
			[]byte(strings.Replace(string(original), "two", "two modified", 1)), 0o644)
	})

	_, stderr := mustRun(t, exitSuccess, "script", "edit", "API/Ping", "--pre")
	if !strings.Contains(stderr, `updated scripts "API/Ping"`) {
		t.Errorf("stderr = %q, want the update confirmation", stderr)
	}

	req := storedRequest(t, "API/Ping")
	if req.Scripts == nil {
		t.Fatal("scripts vanished")
	}
	pre := req.Scripts.PreRequest
	if len(pre) != 2 || pre[0].ID != "pre-1" || pre[1].ID != "pre-2" {
		t.Fatalf("pre entries = %+v, want pre-1 then pre-2", pre)
	}
	if pre[0].Source != "one\n" {
		t.Errorf("pre-1 source = %q, want %q", pre[0].Source, "one\n")
	}
	if pre[1].Source != "two modified\n" {
		t.Errorf("pre-2 source = %q, want %q", pre[1].Source, "two modified\n")
	}
	if !pre[0].Enabled || !pre[1].Enabled {
		t.Error("editing must preserve the enabled state")
	}
	// The other phase is untouched.
	post := req.Scripts.PostResponse
	if len(post) != 1 || post[0].ID != "post-1" || post[0].Source != "posted\n" {
		t.Errorf("post entries = %+v, want post-1 untouched", post)
	}
}

func TestScriptEditDamagedMarker(t *testing.T) {
	t.Chdir(t.TempDir())
	editSetup(t)

	setScripts(t, "API/Ping", &model.Scripts{
		PreRequest: []model.Script{
			{ID: "pre-1", Source: "one\n", Enabled: true},
			{ID: "pre-2", Source: "two\n", Enabled: true},
		},
	})
	before := collectionFileBytes(t)

	t.Setenv("EDITOR", "fake-editor")
	edited := "// ---- req script pre-1 ----\none\ntwo modified\n" // second marker deleted
	overrideRunEditor(t, func(argv []string) error {
		return os.WriteFile(argv[len(argv)-1], []byte(edited), 0o644)
	})

	_, stderr := mustRun(t, exitUsage, "script", "edit", "API/Ping", "--pre")
	if !strings.Contains(stderr, "script separator missing, damaged or reordered") {
		t.Errorf("stderr = %q, want the damaged-marker diagnostic", stderr)
	}
	if !strings.Contains(stderr, "preserved at") {
		t.Errorf("stderr = %q, want the recovery pointer", stderr)
	}

	// The collection file is untouched and the edit is preserved in recovery.
	if after := collectionFileBytes(t); !bytes.Equal(before, after) {
		t.Error("an invalid script edit rewrote the collection file")
	}
	recoveries, err := filepath.Glob(filepath.Join(".req", "recovery", "*.json"))
	if err != nil || len(recoveries) == 0 {
		t.Fatalf("recovery files = %v (err %v), want the edited scripts preserved", recoveries, err)
	}
	found := false
	for _, r := range recoveries {
		if data, rerr := os.ReadFile(r); rerr == nil && string(data) == edited {
			found = true
		}
	}
	if !found {
		t.Errorf("no recovery file holds the edited scripts %q", edited)
	}
	if left := leftoverScriptEdits(t); len(left) != 0 {
		t.Errorf("temp script edit files remain: %v", left)
		for _, p := range left {
			os.Remove(p)
		}
	}
}

func TestScriptEditZeroEntriesUnchanged(t *testing.T) {
	t.Chdir(t.TempDir())
	editSetup(t)

	t.Setenv("EDITOR", "fake-editor")
	overrideRunEditor(t, func(argv []string) error { return nil })

	_, stderr := mustRun(t, exitSuccess, "script", "edit", "API/Ping", "--post")
	if !strings.Contains(stderr, `scripts "API/Ping" unchanged`) {
		t.Errorf("stderr = %q, want the unchanged notice", stderr)
	}
	if req := storedRequest(t, "API/Ping"); req.Scripts != nil {
		t.Errorf("scripts = %+v, want none created", req.Scripts)
	}
	if left := leftoverScriptEdits(t); len(left) != 0 {
		t.Errorf("temp script edit files remain: %v", left)
		for _, p := range left {
			os.Remove(p)
		}
	}
}

func TestScriptEditFolderAndCollectionRoot(t *testing.T) {
	t.Chdir(t.TempDir())
	editSetup(t)
	mustRun(t, exitSuccess, "folder", "create", "API/Auth")

	t.Setenv("EDITOR", "fake-editor")

	// A folder path attaches --pre scripts to the folder.
	overrideRunEditor(t, func(argv []string) error {
		return os.WriteFile(argv[len(argv)-1], []byte("folder-mark\n"), 0o644)
	})
	_, stderr := mustRun(t, exitSuccess, "script", "edit", "API/Auth", "--pre")
	if !strings.Contains(stderr, `updated scripts "API/Auth"`) {
		t.Errorf("folder edit: stderr = %q, want the update confirmation", stderr)
	}
	ws, err := store.Open("")
	if err != nil {
		t.Fatalf("opening workspace: %v", err)
	}
	rp, err := ws.ResolvePath(context.Background(), "API/Auth")
	if err != nil {
		t.Fatalf("resolving API/Auth: %v", err)
	}
	if rp.Item.Folder.Scripts == nil || len(rp.Item.Folder.Scripts.PreRequest) != 1 ||
		rp.Item.Folder.Scripts.PreRequest[0].Source != "folder-mark\n" {
		t.Errorf("folder scripts = %+v, want one folder-mark entry", rp.Item.Folder.Scripts)
	}

	// The collection root accepts --post.
	overrideRunEditor(t, func(argv []string) error {
		return os.WriteFile(argv[len(argv)-1], []byte("coll-mark\n"), 0o644)
	})
	_, stderr = mustRun(t, exitSuccess, "script", "edit", "API", "--post")
	if !strings.Contains(stderr, `updated scripts "API"`) {
		t.Errorf("collection edit: stderr = %q, want the update confirmation", stderr)
	}
	rp, err = ws.ResolvePath(context.Background(), "API")
	if err != nil {
		t.Fatalf("resolving API: %v", err)
	}
	if rp.Collection.Scripts == nil || len(rp.Collection.Scripts.PostResponse) != 1 ||
		rp.Collection.Scripts.PostResponse[0].Source != "coll-mark\n" {
		t.Errorf("collection scripts = %+v, want one coll-mark entry", rp.Collection.Scripts)
	}
	// The request is untouched by both edits.
	if req := storedRequest(t, "API/Ping"); req.Scripts != nil {
		t.Errorf("request scripts = %+v, want none", req.Scripts)
	}
}

func TestRunScriptLifecycle(t *testing.T) {
	t.Chdir(t.TempDir())
	mustRun(t, exitSuccess, "init")
	mustRun(t, exitSuccess, "collection", "create", "API")

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = io.WriteString(w, "pong")
	}))
	defer srv.Close()

	mustRun(t, exitSuccess, "request", "create", "API/Ping", "--method", "GET", "--url", srv.URL)
	setScripts(t, "API", &model.Scripts{
		PreRequest: []model.Script{{ID: "c-pre", Source: `console.log("c-pre")`, Enabled: true}},
	})

	// The collection pre script runs before the send.
	stdout, stderr := mustRun(t, exitSuccess, "run", "API/Ping")
	if stdout != "pong" {
		t.Errorf("stdout = %q, want the body", stdout)
	}
	if !strings.Contains(stderr, "c-pre: c-pre") {
		t.Errorf("stderr = %q, want the collection pre script log", stderr)
	}
	if n := hits.Load(); n != 1 {
		t.Errorf("server hits = %d, want 1", n)
	}

	// --no-scripts skips it and streams the body as usual.
	stdout, stderr = mustRun(t, exitSuccess, "run", "API/Ping", "--no-scripts")
	if stdout != "pong" || strings.Contains(stderr, "c-pre: c-pre") {
		t.Errorf("stdout = %q, stderr = %q, want the body without script logs", stdout, stderr)
	}
	if n := hits.Load(); n != 2 {
		t.Errorf("server hits = %d, want 2", n)
	}

	// A throwing pre script stops everything before the send; the script
	// timeout flag is accepted alongside it.
	setScripts(t, "API", &model.Scripts{
		PreRequest: []model.Script{{ID: "c-pre", Source: `throw new Error("boom")`, Enabled: true}},
	})
	_, stderr = mustRun(t, exitScript, "run", "API/Ping", "--script-timeout", "5s")
	if !strings.Contains(stderr, "script c-pre failed") {
		t.Errorf("stderr = %q, want the script failure", stderr)
	}
	if n := hits.Load(); n != 2 {
		t.Errorf("server hits = %d, want 2 (the request must not be sent)", n)
	}

	// Invalid --script-timeout values fail like --timeout does.
	mustRun(t, exitUsage, "run", "API/Ping", "--script-timeout", "soon")
	mustRun(t, exitUsage, "run", "API/Ping", "--script-timeout", "0s")
	mustRun(t, exitUsage, "run", "API/Ping", "--script-timeout")
}
