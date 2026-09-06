package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"req/internal/model"
	"req/internal/store"
)

// overrideRunEditor replaces runEditor for the duration of the test, so no
// real editor is ever launched.
func overrideRunEditor(t *testing.T, fake func(argv []string) error) {
	t.Helper()
	orig := runEditor
	runEditor = fake
	t.Cleanup(func() { runEditor = orig })
}

// editSetup builds a workspace holding one request, API/Ping.
func editSetup(t *testing.T) {
	t.Helper()
	mustRun(t, exitSuccess, "init")
	mustRun(t, exitSuccess, "collection", "create", "API")
	mustRun(t, exitSuccess, "request", "create", "API/Ping", "--method", "GET", "--url", "https://x/ping")
}

// leftoverTempEdits lists req-edit temp files still present in the OS temp
// directory.
func leftoverTempEdits(t *testing.T) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(os.TempDir(), "req-edit-*.json"))
	if err != nil {
		t.Fatalf("globbing temp edits: %v", err)
	}
	return matches
}

func TestRequestEditHappyPath(t *testing.T) {
	t.Chdir(t.TempDir())
	editSetup(t)
	before := collectionFileBytes(t)

	t.Setenv("EDITOR", "fake-editor")
	t.Setenv("VISUAL", "")

	edited := "{\n  \"method\": \"POST\",\n  \"url\": \"https://x/ping\",\n  \"headers\": [\n    {\n      \"key\": \"X-Edit\",\n      \"value\": \"1\",\n      \"enabled\": true\n    }\n  ]\n}\n"
	var gotArgv []string
	overrideRunEditor(t, func(argv []string) error {
		gotArgv = argv
		return os.WriteFile(argv[len(argv)-1], []byte(edited), 0o644)
	})

	_, stderr := mustRun(t, exitSuccess, "request", "edit", "API/Ping")
	if !strings.Contains(stderr, `updated request "API/Ping"`) {
		t.Errorf("stderr = %q, want the update confirmation", stderr)
	}
	if len(gotArgv) != 2 {
		t.Fatalf("editor argv = %v, want [EDITOR tempfile]", gotArgv)
	}
	if gotArgv[0] != "fake-editor" {
		t.Errorf("editor argv[0] = %q, want the EDITOR value verbatim", gotArgv[0])
	}
	if base := filepath.Base(gotArgv[1]); !strings.HasPrefix(base, "req-edit-") || !strings.HasSuffix(base, ".json") {
		t.Errorf("temp file = %q, want a req-edit-*.json name", gotArgv[1])
	}

	stdout, _ := mustRun(t, exitSuccess, "request", "show", "API/Ping")
	var req model.Request
	if err := json.Unmarshal([]byte(stdout), &req); err != nil {
		t.Fatalf("show output is not JSON: %v\n%s", err, stdout)
	}
	if req.Method != "POST" || req.URL != "https://x/ping" {
		t.Errorf("show = %s %q, want POST https://x/ping", req.Method, req.URL)
	}
	if len(req.Headers) != 1 || req.Headers[0].Key != "X-Edit" || !req.Headers[0].Enabled {
		t.Errorf("show headers = %+v, want one enabled X-Edit entry", req.Headers)
	}
	if after := collectionFileBytes(t); bytes.Equal(before, after) {
		t.Error("the collection file did not change on disk")
	}
	if left := leftoverTempEdits(t); len(left) != 0 {
		t.Errorf("temp edit files remain: %v", left)
		for _, p := range left {
			os.Remove(p)
		}
	}

	// Usage errors.
	mustRun(t, exitUsage, "request", "edit")
	mustRun(t, exitUsage, "request", "edit", "API/Ping", "extra")
	mustRun(t, exitUsage, "request", "edit", "API/Ping", "--bogus")
	mustRun(t, exitUsage, "request", "edit", "API")
	mustRun(t, exitUsage, "request", "edit", "API/Nope")

	// VISUAL is used when EDITOR is blank, and an untouched file means the
	// request is unchanged.
	t.Setenv("EDITOR", "   ")
	t.Setenv("VISUAL", "visual-editor")
	overrideRunEditor(t, func(argv []string) error {
		if argv[0] != "visual-editor" {
			t.Errorf("editor = %q, want the VISUAL fallback", argv[0])
		}
		return nil
	})
	_, stderr = mustRun(t, exitSuccess, "request", "edit", "API/Ping")
	if !strings.Contains(stderr, `request "API/Ping" unchanged`) {
		t.Errorf("stderr = %q, want the unchanged notice", stderr)
	}
	if left := leftoverTempEdits(t); len(left) != 0 {
		t.Errorf("temp edit files remain: %v", left)
		for _, p := range left {
			os.Remove(p)
		}
	}
}

func TestRequestEditUnchanged(t *testing.T) {
	t.Chdir(t.TempDir())
	editSetup(t)
	before := collectionFileBytes(t)

	t.Setenv("EDITOR", "fake-editor")
	overrideRunEditor(t, func(argv []string) error { return nil })

	_, stderr := mustRun(t, exitSuccess, "request", "edit", "API/Ping")
	if !strings.Contains(stderr, `request "API/Ping" unchanged`) {
		t.Errorf("stderr = %q, want the unchanged notice", stderr)
	}
	if after := collectionFileBytes(t); !bytes.Equal(before, after) {
		t.Error("an unchanged edit rewrote the collection file")
	}
	if left := leftoverTempEdits(t); len(left) != 0 {
		t.Errorf("temp edit files remain: %v", left)
		for _, p := range left {
			os.Remove(p)
		}
	}
}

func TestRequestEditInvalidJSON(t *testing.T) {
	t.Chdir(t.TempDir())
	editSetup(t)
	before := collectionFileBytes(t)

	t.Setenv("EDITOR", "fake-editor")
	edits := []string{
		"{oops\n",
		"{\n  \"method\": \"GET\",\n  \"url\": \"http://x/\",\n  \"bogus\": 1\n}\n",
	}
	for _, edited := range edits {
		overrideRunEditor(t, func(argv []string) error {
			return os.WriteFile(argv[len(argv)-1], []byte(edited), 0o644)
		})
		_, stderr := mustRun(t, exitUsage, "request", "edit", "API/Ping")
		if !strings.Contains(stderr, "edited request is invalid") {
			t.Errorf("edit %q: stderr = %q, want the invalid-edit diagnostic", edited, stderr)
		}
	}

	// The edited bytes were preserved under .req/recovery/.
	recoveries, err := filepath.Glob(filepath.Join(".req", "recovery", "*.json"))
	if err != nil || len(recoveries) < len(edits) {
		t.Fatalf("recovery files = %v (err %v), want at least %d", recoveries, err, len(edits))
	}
	last := edits[len(edits)-1]
	found := false
	for _, r := range recoveries {
		if data, rerr := os.ReadFile(r); rerr == nil && string(data) == last {
			found = true
		}
	}
	if !found {
		t.Errorf("no recovery file holds the edited bytes %q", last)
	}

	// The stored request is untouched.
	stdout, _ := mustRun(t, exitSuccess, "request", "show", "API/Ping")
	if !strings.Contains(stdout, `"method": "GET"`) {
		t.Errorf("show = %q, want the stored GET request", stdout)
	}
	if after := collectionFileBytes(t); !bytes.Equal(before, after) {
		t.Error("an invalid edit rewrote the collection file")
	}
	if left := leftoverTempEdits(t); len(left) != 0 {
		t.Errorf("temp edit files remain: %v", left)
		for _, p := range left {
			os.Remove(p)
		}
	}
}

func TestEditorMissingEnv(t *testing.T) {
	t.Chdir(t.TempDir())
	editSetup(t)

	t.Setenv("EDITOR", "")
	t.Setenv("VISUAL", "  ") // whitespace-only counts as unset

	_, stderr := mustRun(t, exitUsage, "request", "edit", "API/Ping")
	if !strings.Contains(stderr, "EDITOR") || !strings.Contains(stderr, "VISUAL") {
		t.Errorf("stderr = %q, want both EDITOR and VISUAL named", stderr)
	}
	if !strings.Contains(stderr, "notepad") || !strings.Contains(stderr, "code --wait") {
		t.Errorf("stderr = %q, want a setup example", stderr)
	}
	if left := leftoverTempEdits(t); len(left) != 0 {
		t.Errorf("temp edit files remain: %v", left)
		for _, p := range left {
			os.Remove(p)
		}
	}
}

func TestEditorFails(t *testing.T) {
	t.Chdir(t.TempDir())
	editSetup(t)
	before := collectionFileBytes(t)

	t.Setenv("EDITOR", "fake-editor")
	overrideRunEditor(t, func(argv []string) error {
		return errors.New("fake editor exited with status 1")
	})

	_, stderr := mustRun(t, exitUsage, "request", "edit", "API/Ping")
	if !strings.Contains(stderr, "editor failed") {
		t.Errorf("stderr = %q, want the editor-failure diagnostic", stderr)
	}
	// The temp file is kept so the edit is not lost, and the message names it.
	left := leftoverTempEdits(t)
	if len(left) == 0 {
		t.Fatal("the temp edit file was removed after an editor failure")
	}
	if !strings.Contains(stderr, left[0]) {
		t.Errorf("stderr = %q, want it to name the preserved temp file %q", stderr, left[0])
	}
	for _, p := range left { // clean up the preserved temp file
		os.Remove(p)
	}
	if after := collectionFileBytes(t); !bytes.Equal(before, after) {
		t.Error("a failed editor run changed the collection file")
	}
}

func TestEditorConflictRecovery(t *testing.T) {
	t.Chdir(t.TempDir())
	editSetup(t)

	t.Setenv("EDITOR", "fake-editor")
	overrideRunEditor(t, func(argv []string) error {
		// (a) A competing writer saves while the editor is open, standing in
		// for another req process.
		ws, err := store.Open("")
		if err != nil {
			return err
		}
		ctx := context.Background()
		rp, err := ws.ResolvePath(ctx, "API/Ping")
		if err != nil {
			return err
		}
		competing := model.Request{Method: "PATCH", URL: "https://x/competing"}
		if err := ws.UpdateRequest(ctx, "API/Ping", competing, rp.Rev); err != nil {
			return err
		}
		// (b) The user's edit is a different valid request.
		losing := "{\n  \"method\": \"PUT\",\n  \"url\": \"https://x/losing\"\n}\n"
		return os.WriteFile(argv[len(argv)-1], []byte(losing), 0o644)
	})

	_, stderr := mustRun(t, exitStorage, "request", "edit", "API/Ping")
	if !strings.Contains(stderr, "changed on disk") {
		t.Errorf("stderr = %q, want the conflict diagnostic", stderr)
	}
	if !strings.Contains(stderr, "recovery") {
		t.Errorf("stderr = %q, want the recovery path named", stderr)
	}

	// The competing writer's content is what survived on disk.
	stdout, _ := mustRun(t, exitSuccess, "request", "show", "API/Ping")
	if !strings.Contains(stdout, "PATCH") || !strings.Contains(stdout, "https://x/competing") {
		t.Errorf("show = %q, want the competing writer's content", stdout)
	}

	// The losing edit's candidate is preserved under .req/recovery/.
	recoveries, err := filepath.Glob(filepath.Join(".req", "recovery", "*.json"))
	if err != nil || len(recoveries) == 0 {
		t.Fatalf("recovery files = %v (err %v), want the losing edit preserved", recoveries, err)
	}
	found := false
	for _, r := range recoveries {
		if data, rerr := os.ReadFile(r); rerr == nil && strings.Contains(string(data), "https://x/losing") {
			found = true
		}
	}
	if !found {
		t.Errorf("no recovery file contains the losing edit (files: %v)", recoveries)
	}
}
