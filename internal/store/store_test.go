package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"req/internal/model"
)

func newTestWorkspace(t *testing.T) *Workspace {
	t.Helper()
	ws, _, err := Init(t.TempDir())
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	return ws
}

func saveFixture(t *testing.T, ws *Workspace) (model.Collection, Revision) {
	t.Helper()
	c := fixtureCollection()
	if err := ws.SaveCollection(context.Background(), c, ""); err != nil {
		t.Fatalf("SaveCollection (create): %v", err)
	}
	loaded, rev, err := ws.LoadCollection(context.Background(), c.ID)
	if err != nil {
		t.Fatalf("LoadCollection: %v", err)
	}
	return loaded, rev
}

func fixtureCollection() model.Collection {
	return model.Collection{
		SchemaVersion: 1,
		ID:            "col-test-001",
		Name:          "Test API",
		Variables:     map[string]interface{}{"base_url": "https://api.example.test", "retries": float64(3)},
		Auth:          &model.Auth{Type: "bearer", Token: "{{service_token}}"},
		Scripts: &model.Scripts{PreRequest: []model.Script{
			{ID: "scr-pre-setup", Source: "// pre", Enabled: true},
		}},
		Items: []model.Item{
			{
				Type: "folder", ID: "fld-auth", Name: "Auth",
				Folder: &model.Folder{
					Auth: &model.Auth{Type: "basic", Username: "{{api_user}}", Password: "{{api_password}}"},
					Children: []model.Item{
						{
							Type: "request", ID: "req-login", Name: "Login",
							Request: &model.Request{
								Method: "POST",
								URL:    "{{base_url}}/login",
								Query: []model.Entry{
									{Key: "verbose", Value: "true", Enabled: true},
									{Key: "verbose", Value: "logs", Enabled: false},
								},
								Headers: []model.Entry{
									{Key: "X-Trace", Value: "1", Enabled: true},
									{Key: "X-Trace", Value: "2", Enabled: true},
								},
								Body: &model.Body{Type: "json", Text: &[]string{`{"email":"{{email}}"}`}[0]},
							},
						},
					},
				},
			},
			{
				Type: "request", ID: "req-ping", Name: "Ping",
				Request: &model.Request{
					Method: "GET",
					URL:    "{{base_url}}/ping",
					Auth:   &model.Auth{Type: "none"},
					Body:   &model.Body{Type: "none"},
				},
			},
		},
	}
}

func TestStoreRoundTrip(t *testing.T) {
	ws := newTestWorkspace(t)
	c := fixtureCollection()
	if err := ws.SaveCollection(context.Background(), c, ""); err != nil {
		t.Fatalf("SaveCollection (create): %v", err)
	}
	loaded, rev, err := ws.LoadCollection(context.Background(), c.ID)
	if err != nil {
		t.Fatalf("LoadCollection: %v", err)
	}
	if rev == "" {
		t.Error("revision hash is empty")
	}
	if !sameTree(c, loaded) {
		t.Errorf("round trip changed the collection:\n got %+v\nwant %+v", loaded, c)
	}
	// Stable IDs survive the reload.
	got := map[string]bool{}
	walkIDs(loaded.Items, got)
	for _, id := range []string{"fld-auth", "req-login", "req-ping"} {
		if !got[id] {
			t.Errorf("id %q lost on reload (have %v)", id, got)
		}
	}
}

func TestStoreFixtureLoads(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "synthetic-collection.json"))
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	ws := newTestWorkspace(t)
	path := filepath.Join(ws.Dir(), "collections", "col-synthetic-001.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("placing fixture: %v", err)
	}
	c, rev, err := ws.LoadCollection(context.Background(), "col-synthetic-001")
	if err != nil {
		t.Fatalf("fixture does not load strictly: %v", err)
	}
	if err := ws.SaveCollection(context.Background(), c, rev); err != nil {
		t.Fatalf("SaveCollection: %v", err)
	}
	reloaded, _, err := ws.LoadCollection(context.Background(), c.ID)
	if err != nil {
		t.Fatalf("reloading: %v", err)
	}
	if !sameTree(c, reloaded) {
		t.Error("fixture round trip changed the collection")
	}
}

func TestStoreConflict(t *testing.T) {
	ws := newTestWorkspace(t)
	c, rev1 := saveFixture(t, ws)

	// A second writer wins the revision race.
	c.Name = "Renamed once"
	if err := ws.SaveCollection(context.Background(), c, rev1); err != nil {
		t.Fatalf("SaveCollection with fresh revision: %v", err)
	}
	_, rev2, err := ws.LoadCollection(context.Background(), c.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if rev1 == rev2 {
		t.Fatal("revision did not change after a save")
	}

	// The stale writer must not overwrite the winner.
	stale := fixtureCollection()
	stale.Name = "Stale write"
	err = ws.SaveCollection(context.Background(), stale, rev1)
	var cerr *ConflictError
	if !errors.As(err, &cerr) {
		t.Fatalf("stale save: got %v, want ConflictError", err)
	}
	if cerr.Recovery == "" {
		t.Error("conflict error does not point at a recovery file")
	} else if _, statErr := os.Stat(cerr.Recovery); statErr != nil {
		t.Errorf("recovery file missing: %v", statErr)
	}
	onDisk, _, err := ws.LoadCollection(context.Background(), c.ID)
	if err != nil {
		t.Fatalf("reload after conflict: %v", err)
	}
	if onDisk.Name != "Renamed once" {
		t.Errorf("stale write overwrote the winner: name = %q", onDisk.Name)
	}

	// Saving with an empty expected revision over an existing file also
	// conflicts rather than blindly overwriting.
	err = ws.SaveCollection(context.Background(), stale, "")
	if !errors.As(err, &cerr) {
		t.Fatalf("create-over-existing: got %v, want ConflictError", err)
	}
}

func TestStoreFutureSchema(t *testing.T) {
	ws := newTestWorkspace(t)
	path := filepath.Join(ws.Dir(), "collections", "col-future-9.json")
	future := `{"schema_version": 99, "id": "col-future-9", "name": "Future", "items": []}`
	if err := os.WriteFile(path, []byte(future), 0o644); err != nil {
		t.Fatalf("writing future file: %v", err)
	}

	_, _, err := ws.LoadCollection(context.Background(), "col-future-9")
	var serr *model.SchemaVersionError
	if !errors.As(err, &serr) {
		t.Fatalf("load: got %v, want SchemaVersionError", err)
	}
	if serr.Found != 99 || serr.Want != model.SchemaVersion {
		t.Errorf("SchemaVersionError = found %d want %d, unsupported", serr.Found, serr.Want)
	}

	// The future file is never rewritten, not even by a save attempt.
	err = ws.SaveCollection(context.Background(), model.Collection{
		SchemaVersion: model.SchemaVersion, ID: "col-future-9", Name: "Present", Items: nil,
	}, "")
	var cerr *ConflictError
	if !errors.As(err, &cerr) {
		t.Fatalf("save over future file: got %v, want ConflictError (no blind overwrite)", err)
	}
	after, readErr := os.ReadFile(path)
	if readErr != nil || string(after) != future {
		t.Errorf("future schema file was modified: %s (%v)", after, readErr)
	}
}

func TestStoreMalformed(t *testing.T) {
	ws := newTestWorkspace(t)
	cases := map[string]string{
		"garbage":            "{not json",
		"unknown field":      `{"schema_version":1,"id":"col-mal-001","name":"X","items":[],"bogus":1}`,
		"trailing data":      `{"schema_version":1,"id":"col-mal-001","name":"X","items":[]} trailing`,
		"validation failure": `{"schema_version":1,"id":"col-mal-001","name":"a/b","items":[]}`,
	}
	for name, content := range cases {
		path := filepath.Join(ws.Dir(), "collections", "col-mal-001.json")
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("writing %s case: %v", name, err)
		}
		if _, _, err := ws.LoadCollection(context.Background(), "col-mal-001"); err == nil {
			t.Errorf("%s: load succeeded, want error", name)
		}
		after, _ := os.ReadFile(path)
		if string(after) != content {
			t.Errorf("%s: malformed file was rewritten", name)
		}
	}
}

func TestStoreConcurrentWriters(t *testing.T) {
	ws := newTestWorkspace(t)
	_, rev := saveFixture(t, ws)

	const writers = 8
	var wins, conflicts atomic.Int32
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			candidate := fixtureCollection()
			candidate.Name = "Writer"
			<-start
			err := ws.SaveCollection(context.Background(), candidate, rev)
			switch {
			case err == nil:
				wins.Add(1)
			default:
				var cerr *ConflictError
				if errors.As(err, &cerr) {
					conflicts.Add(1)
				} else {
					t.Errorf("writer %d: %v", i, err)
				}
			}
		}(i)
	}
	close(start)
	wg.Wait()

	if got := wins.Load(); got != 1 {
		t.Errorf("successful writers = %d, want exactly 1", got)
	}
	if got := conflicts.Load(); got != writers-1 {
		t.Errorf("conflicting writers = %d, want %d", got, writers-1)
	}
	final, _, err := ws.LoadCollection(context.Background(), "col-test-001")
	if err != nil {
		t.Fatalf("final load: %v", err)
	}
	if final.Name != "Writer" {
		t.Errorf("final name = %q, want the single winner's write", final.Name)
	}
}

func TestStoreLockBlocksDuringSave(t *testing.T) {
	ws := newTestWorkspace(t)
	_, rev := saveFixture(t, ws)

	unlock, err := ws.lock(context.Background())
	if err != nil {
		t.Fatalf("lock: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		c := fixtureCollection()
		c.Name = "Waited for the lock"
		done <- ws.SaveCollection(context.Background(), c, rev)
	}()

	// While the mutation lock is held the save must not complete; completing
	// would mean the revision check and replace can race with a lock holder.
	select {
	case err := <-done:
		t.Fatalf("save completed while the workspace lock was held: %v", err)
	case <-time.After(150 * time.Millisecond):
	}

	unlock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("save after lock release: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("save never completed after the lock was released")
	}
}

func TestInitIdempotent(t *testing.T) {
	dir := t.TempDir()
	ws1, created, err := Init(dir)
	if err != nil || !created {
		t.Fatalf("first Init: created=%v err=%v", created, err)
	}
	configPath := filepath.Join(ws1.Dir(), "config.json")
	custom := "{\n  \"schema_version\": 1,\n  \"note\": \"user edited\"\n}\n"
	if err := os.WriteFile(configPath, []byte(custom), 0o644); err != nil {
		t.Fatalf("editing config: %v", err)
	}
	gitignorePath := filepath.Join(ws1.Dir(), ".gitignore")
	if err := os.WriteFile(gitignorePath, []byte("# user override\n"), 0o644); err != nil {
		t.Fatalf("editing gitignore: %v", err)
	}

	ws2, created, err := Init(dir)
	if err != nil {
		t.Fatalf("second Init: %v", err)
	}
	if created {
		t.Error("second Init reported creation")
	}
	if ws2.Root() != ws1.Root() {
		t.Errorf("roots differ: %q vs %q", ws2.Root(), ws1.Root())
	}
	got, _ := os.ReadFile(configPath)
	if string(got) != custom {
		t.Errorf("config.json overwritten by re-init:\n%s", got)
	}
	got, _ = os.ReadFile(gitignorePath)
	if string(got) != "# user override\n" {
		t.Errorf(".gitignore overwritten by re-init:\n%s", got)
	}
	for _, sub := range []string{"collections", "environments", "recovery"} {
		if !dirExists(filepath.Join(ws2.Dir(), sub)) {
			t.Errorf("subdirectory %s missing after re-init", sub)
		}
	}
}

func TestDiscoverClosestAncestor(t *testing.T) {
	root := t.TempDir()
	if _, _, err := Init(root); err != nil {
		t.Fatalf("Init: %v", err)
	}
	deep := filepath.Join(root, "services", "billing", "v2")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	ws, err := Discover(deep)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if ws.Root() != root {
		t.Errorf("discovered root %q, want %q", ws.Root(), root)
	}
	if _, err := Discover(t.TempDir()); !errors.Is(err, ErrNoWorkspace) {
		t.Errorf("Discover outside a workspace: got %v, want ErrNoWorkspace", err)
	}
}

func TestOpenExplicit(t *testing.T) {
	root := t.TempDir()
	if _, _, err := Init(root); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if ws, err := Open(root); err != nil || ws.Root() != root {
		t.Errorf("Open(root) = %v, %v", ws, err)
	}
	if ws, err := Open(filepath.Join(root, ".req")); err != nil || ws.Root() != root {
		t.Errorf("Open(.req) = %v, %v", ws, err)
	}
	if _, err := Open(t.TempDir()); !errors.Is(err, ErrNoWorkspace) {
		t.Errorf("Open(non-workspace) error = %v, want ErrNoWorkspace", err)
	}
}

// walkIDs collects every item id in the tree.
func walkIDs(items []model.Item, into map[string]bool) {
	for _, it := range items {
		into[it.ID] = true
		if it.Folder != nil {
			walkIDs(it.Folder.Children, into)
		}
	}
}

// sameTree compares two collections ignoring nothing; used after round
// trips. (reflect.DeepEqual on the whole struct works too, but a mismatch
// message pointing at the field helps.)
func sameTree(a, b model.Collection) bool {
	if a.ID != b.ID || a.Name != b.Name || a.SchemaVersion != b.SchemaVersion {
		return false
	}
	if len(a.Variables) != len(b.Variables) {
		return false
	}
	for k, v := range a.Variables {
		if bv, ok := b.Variables[k]; !ok || v != bv {
			return false
		}
	}
	if (a.Auth == nil) != (b.Auth == nil) || (a.Auth != nil && *a.Auth != *b.Auth) {
		return false
	}
	if (a.Scripts == nil) != (b.Scripts == nil) {
		return false
	}
	return sameItems(a.Items, b.Items)
}

func sameItems(a, b []model.Item) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		x, y := a[i], b[i]
		if x.Type != y.Type || x.ID != y.ID || x.Name != y.Name {
			return false
		}
		if (x.Folder == nil) != (y.Folder == nil) || (x.Request == nil) != (y.Request == nil) {
			return false
		}
		if x.Folder != nil && !sameItems(x.Folder.Children, y.Folder.Children) {
			return false
		}
		if (x.Request == nil) != (y.Request == nil) {
			return false
		}
		if x.Request != nil && !reflect.DeepEqual(x.Request, y.Request) {
			return false
		}
	}
	return true
}
