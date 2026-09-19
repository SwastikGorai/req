package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/SwastikGorai/req/internal/model"
	"github.com/SwastikGorai/req/internal/store"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestEnvironmentSwitchingAndAuthInheritance(t *testing.T) {
	t.Chdir(t.TempDir())
	ctx := context.Background()
	mustRun(t, 0, "init")
	mustRun(t, 0, "collection", "create", "API")
	mustRun(t, 0, "folder", "create", "API/Outer/Inner", "--parents")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		fmtBody := r.Header.Get("Authorization") + "|" + r.URL.Query().Get("q") + "|" + string(body)
		io.WriteString(w, fmtBody)
	}))
	defer srv.Close()
	mustRun(t, 0, "request", "create", "API/Outer/Inner/Ping", "--method", "POST", "--url", "{{base}}", "--query", "q={{value}}", "--json", "{{payload}}")
	w, err := store.Discover("")
	if err != nil {
		t.Fatal(err)
	}
	rp, err := w.ResolvePath(ctx, "API")
	if err != nil {
		t.Fatal(err)
	}
	rp.Collection.Variables = map[string]any{"base": srv.URL, "value": "collection", "payload": map[string]any{"ok": true}}
	rp.Collection.Auth = &model.Auth{Type: "bearer", Token: "collection"}
	rp.Collection.Items[0].Folder.Auth = &model.Auth{Type: "bearer", Token: "{{token}}"}
	if err := w.SaveCollection(ctx, rp.Collection, rp.Rev); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"local", "stage"} {
		mustRun(t, 0, "env", "create", name)
		e, rev, err := w.LoadEnvironment(ctx, name)
		if err != nil {
			t.Fatal(err)
		}
		e.Variables = map[string]any{"value": name, "token": name, "base": srv.URL}
		if err := w.SaveEnvironment(ctx, e, rev); err != nil {
			t.Fatal(err)
		}
	}
	before := collectionFileBytes(t)
	for _, tc := range []struct {
		flags []string
		want  string
	}{
		{[]string{"--env", "local"}, `Bearer local|local|{"ok":true}`},
		{[]string{"--env", "stage"}, `Bearer stage|stage|{"ok":true}`},
		{[]string{"--env", "local", "--var", "value=cli", "--var", "value=last", "--bearer", "cli"}, `Bearer cli|last|{"ok":true}`},
		{[]string{"--no-auth"}, `|collection|{"ok":true}`},
		{[]string{"--basic-user", "user", "--basic-password", "pass"}, `Basic dXNlcjpwYXNz|collection|{"ok":true}`},
		{[]string{"-H", "Authorization: explicit"}, `explicit|collection|{"ok":true}`},
	} {
		args := append([]string{"run", "API/Outer/Inner/Ping"}, tc.flags...)
		got, _ := mustRun(t, 0, args...)
		if got != tc.want {
			t.Fatalf("%v: %q != %q", args, got, tc.want)
		}
	}
	if !bytes.Equal(before, collectionFileBytes(t)) {
		t.Fatal("run changed saved collection")
	}
	mustRun(t, 0, "request", "create", "API/Outer/Inner/Own", "--method", "GET", "--url", srv.URL, "--bearer", "{{token}}")
	if got, _ := mustRun(t, 0, "run", "API/Outer/Inner/Own", "--env", "stage"); got != "Bearer stage||" {
		t.Fatal(got)
	}
	mustRun(t, 0, "request", "create", "API/Outer/Inner/None", "--method", "GET", "--url", srv.URL, "--no-auth")
	if got, _ := mustRun(t, 0, "run", "API/Outer/Inner/None"); got != "||" {
		t.Fatal(got)
	}
	got, _ := mustRun(t, 0, "--workspace", w.Root(), "send", "POST", "{{base}}", "--env", "local", "--bearer", "{{token}}", "--query", "q={{value}}", "--json", "{{data}}", "--var", `data={"direct":true}`)
	if got != `Bearer local|local|{"direct":true}` {
		t.Fatal(got)
	}
	t.Setenv("REQ_TEST_URL", srv.URL)
	t.Chdir(t.TempDir())
	got, _ = mustRun(t, 0, "send", "GET", "{{env:REQ_TEST_URL}}", "--var", "v=standalone", "--query", "q={{v}}")
	if got != "|standalone|" {
		t.Fatal(got)
	}
}

func TestVariableMissingNoSend(t *testing.T) {
	t.Chdir(t.TempDir())
	mustRun(t, 0, "init")
	mustRun(t, 0, "collection", "create", "API")
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer srv.Close()
	mustRun(t, 0, "request", "create", "API/Ping", "--method", "POST", "--url", srv.URL)
	for _, flags := range [][]string{
		{"--query", "q={{missing}}"}, {"--body", "{{missing}}"}, {"--bearer", "{{missing}}"},
		{"--json", "{{value}}", "--var", "value=invalid"}, {"--var", "v={{nested}}", "--query", "q={{v}}"},
		{"--bearer", "x", "--no-auth"}, {"--basic-user", "x"}, {"--var", "env:X=bad"},
		{"--header", "X-Test: {{v}}", "--var", "v=bad\r\nInjected: yes"},
	} {
		mustRun(t, 2, append([]string{"send", "POST", srv.URL}, flags...)...)
		mustRun(t, 2, append([]string{"run", "API/Ping"}, flags...)...)
	}
	if calls.Load() != 0 {
		t.Fatalf("invalid requests sent: %d", calls.Load())
	}
}

func TestEnvironmentEditorAndCRUD(t *testing.T) {
	t.Chdir(t.TempDir())
	mustRun(t, 0, "init")
	mustRun(t, 0, "env", "create", "local")
	mustRun(t, 2, "env", "create", "local")
	mustRun(t, 2, "env", "create", "../bad")
	if got, _ := mustRun(t, 0, "env", "list"); got != "local\n" {
		t.Fatal(got)
	}
	t.Setenv("EDITOR", `"fake editor" --wait`)
	overrideRunEditor(t, func(argv []string) error {
		if argv[0] != "fake editor" {
			t.Fatal(argv)
		}
		return os.WriteFile(argv[len(argv)-1], []byte(`{"schema_version":1,"name":"local","variables":{"token":"edited"}}`), 0600)
	})
	mustRun(t, 0, "env", "edit", "local")
	w, _ := store.Discover("")
	e, _, err := w.LoadEnvironment(context.Background(), "local")
	if err != nil || e.Variables["token"] != "edited" {
		t.Fatalf("%v %v", e, err)
	}
	overrideRunEditor(t, func(argv []string) error { return os.WriteFile(argv[len(argv)-1], []byte(`{"unknown":true}`), 0600) })
	_, stderr := mustRun(t, 2, "env", "edit", "local")
	if !strings.Contains(stderr, "preserved at") {
		t.Fatal(stderr)
	}
	overrideRunEditor(t, func(argv []string) error {
		e, rev, err := w.LoadEnvironment(context.Background(), "local")
		if err != nil {
			return err
		}
		e.Variables["token"] = "winner"
		if err := w.SaveEnvironment(context.Background(), e, rev); err != nil {
			return err
		}
		e.Variables["token"] = "loser"
		b, _ := json.Marshal(e)
		return os.WriteFile(argv[len(argv)-1], b, 0600)
	})
	_, stderr = mustRun(t, 7, "env", "edit", "local")
	if !strings.Contains(stderr, "preserved at") {
		t.Fatal(stderr)
	}
	e, _, _ = w.LoadEnvironment(context.Background(), "local")
	if e.Variables["token"] != "winner" {
		t.Fatal(e)
	}
	files, _ := filepath.Glob(filepath.Join(w.Dir(), "recovery", "env-local-*.json"))
	if len(files) < 2 {
		t.Fatal(files)
	}
	mustRun(t, 0, "env", "delete", "local")
	mustRun(t, 2, "env", "delete", "local")
}
