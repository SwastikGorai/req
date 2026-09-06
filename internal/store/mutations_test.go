package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"req/internal/model"
)

func TestCreateCollectionDuplicateName(t *testing.T) {
	ws := newTestWorkspace(t)
	ctx := context.Background()
	first, err := ws.CreateCollection(ctx, "My API")
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	file := filepath.Join(ws.Dir(), "collections", first.ID+".json")
	before, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("reading stored file: %v", err)
	}

	if _, err := ws.CreateCollection(ctx, "My API"); !errors.Is(err, ErrDuplicateName) {
		t.Fatalf("duplicate CreateCollection error = %v, want ErrDuplicateName", err)
	}

	after, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("re-reading stored file: %v", err)
	}
	if string(before) != string(after) {
		t.Error("the failed duplicate create modified the stored file")
	}

	// Invalid names are rejected before any duplicate check or write.
	for _, name := range []string{"A/B", `A\B`, "", ".", ".."} {
		if _, err := ws.CreateCollection(ctx, name); err == nil || errors.Is(err, ErrDuplicateName) {
			t.Errorf("CreateCollection(%q) error = %v, want a name validation failure (not ErrDuplicateName)", name, err)
		}
	}
	colls, err := ws.ListCollections(ctx)
	if err != nil {
		t.Fatalf("ListCollections: %v", err)
	}
	if len(colls) != 1 || colls[0].Name != "My API" {
		t.Errorf("collections after failed creates = %+v, want only My API", colls)
	}
}

func TestListCollectionsSorted(t *testing.T) {
	ws := newTestWorkspace(t)
	ctx := context.Background()
	for _, name := range []string{"Zebra", "Apple", "Mango"} {
		if _, err := ws.CreateCollection(ctx, name); err != nil {
			t.Fatalf("CreateCollection(%q): %v", name, err)
		}
	}
	colls, err := ws.ListCollections(ctx)
	if err != nil {
		t.Fatalf("ListCollections: %v", err)
	}
	var got []string
	for _, c := range colls {
		got = append(got, c.Name)
	}
	want := []string{"Apple", "Mango", "Zebra"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ListCollections names = %v, want %v", got, want)
	}
}

func TestCreateFolderParents(t *testing.T) {
	ws := newTestWorkspace(t)
	ctx := context.Background()
	col, err := ws.CreateCollection(ctx, "API")
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}

	// Without parents a missing intermediate fails and writes nothing.
	if _, err := ws.CreateFolder(ctx, "API/A/B", false); !errors.Is(err, ErrNotFound) {
		t.Fatalf("CreateFolder(API/A/B, parents=false) error = %v, want ErrNotFound", err)
	}
	stored, _, err := ws.LoadCollection(ctx, col.ID)
	if err != nil {
		t.Fatalf("LoadCollection: %v", err)
	}
	if len(stored.Items) != 0 {
		t.Errorf("the failed create wrote items: %+v", stored.Items)
	}

	// With parents the missing chain is created, innermost last.
	created, err := ws.CreateFolder(ctx, "API/A/B", true)
	if err != nil {
		t.Fatalf("CreateFolder(API/A/B, parents=true): %v", err)
	}
	if want := []string{"A", "B"}; !reflect.DeepEqual(created, want) {
		t.Errorf("created = %v, want %v", created, want)
	}
	stored, _, err = ws.LoadCollection(ctx, col.ID)
	if err != nil {
		t.Fatalf("LoadCollection: %v", err)
	}
	if len(stored.Items) != 1 || stored.Items[0].Name != "A" || stored.Items[0].Folder == nil ||
		len(stored.Items[0].Folder.Children) != 1 || stored.Items[0].Folder.Children[0].Name != "B" {
		t.Errorf("stored tree = %+v, want API/A/B", stored.Items)
	}

	// Creating the now-existing folder again is a duplicate.
	if _, err := ws.CreateFolder(ctx, "API/A", false); !errors.Is(err, ErrDuplicateName) {
		t.Errorf("CreateFolder(API/A) again error = %v, want ErrDuplicateName", err)
	}
}

func TestCreateFolderDuplicateName(t *testing.T) {
	ws := newTestWorkspace(t)
	ctx := context.Background()
	if _, err := ws.CreateCollection(ctx, "API"); err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	if _, err := ws.CreateFolder(ctx, "API/Auth", false); err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}

	// A request named like the folder collides: sibling names are unique
	// across item types.
	err := ws.CreateRequest(ctx, "API/Auth", model.Request{Method: "GET", URL: "https://api.example.test/ping"}, false)
	if !errors.Is(err, ErrDuplicateName) {
		t.Errorf("CreateRequest on an existing folder name error = %v, want ErrDuplicateName", err)
	}

	// A folder with the same name collides too.
	if _, err := ws.CreateFolder(ctx, "API/Auth", false); !errors.Is(err, ErrDuplicateName) {
		t.Errorf("duplicate CreateFolder error = %v, want ErrDuplicateName", err)
	}
}

func TestCreateRequestAtRootAndNested(t *testing.T) {
	ws := newTestWorkspace(t)
	ctx := context.Background()
	col, err := ws.CreateCollection(ctx, "API")
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	ping := model.Request{Method: "GET", URL: "{{base_url}}/ping"}
	if err := ws.CreateRequest(ctx, "API/Ping", ping, false); err != nil {
		t.Fatalf("CreateRequest(Ping): %v", err)
	}
	login := model.Request{Method: "POST", URL: "{{base_url}}/login"}
	if err := ws.CreateRequest(ctx, "API/Auth/Login", login, true); err != nil {
		t.Fatalf("CreateRequest(Login): %v", err)
	}

	stored, _, err := ws.LoadCollection(ctx, col.ID)
	if err != nil {
		t.Fatalf("LoadCollection: %v", err)
	}
	if len(stored.Items) != 2 {
		t.Fatalf("root items = %d, want 2 (%+v)", len(stored.Items), stored.Items)
	}
	pingItem, authItem := stored.Items[0], stored.Items[1]
	// Whatever was created first stays first.
	if pingItem.Name != "Ping" || pingItem.Type != "request" {
		t.Errorf("first root item = %s %q, want request Ping", pingItem.Type, pingItem.Name)
	}
	if authItem.Name != "Auth" || authItem.Type != "folder" || authItem.Folder == nil {
		t.Errorf("second root item = %s %q, want folder Auth", authItem.Type, authItem.Name)
	}
	if !strings.HasPrefix(pingItem.ID, "req-") {
		t.Errorf("Ping id %q lacks the req- prefix", pingItem.ID)
	}
	if !strings.HasPrefix(authItem.ID, "fld-") {
		t.Errorf("Auth id %q lacks the fld- prefix", authItem.ID)
	}
	if pingItem.Request == nil || pingItem.Request.Method != ping.Method || pingItem.Request.URL != ping.URL {
		t.Errorf("Ping request = %+v, want %+v", pingItem.Request, ping)
	}
	if len(authItem.Folder.Children) != 1 {
		t.Fatalf("Auth children = %d, want 1", len(authItem.Folder.Children))
	}
	loginItem := authItem.Folder.Children[0]
	if loginItem.Name != "Login" || loginItem.Type != "request" || !strings.HasPrefix(loginItem.ID, "req-") {
		t.Errorf("Auth child = %+v, want request Login with a req- id", loginItem)
	}
	if loginItem.Request == nil || loginItem.Request.Method != login.Method || loginItem.Request.URL != login.URL {
		t.Errorf("Login request = %+v, want %+v", loginItem.Request, login)
	}
}

func TestCreateOnMissingCollection(t *testing.T) {
	ws := newTestWorkspace(t)
	ctx := context.Background()

	// parents never creates the collection itself.
	if _, err := ws.CreateFolder(ctx, "Nope/A", true); !errors.Is(err, ErrNotFound) {
		t.Errorf("CreateFolder on a missing collection (parents=true) error = %v, want ErrNotFound", err)
	}
	if err := ws.CreateRequest(ctx, "Nope/Ping", model.Request{Method: "GET", URL: "https://api.example.test"}, true); !errors.Is(err, ErrNotFound) {
		t.Errorf("CreateRequest on a missing collection (parents=true) error = %v, want ErrNotFound", err)
	}

	colls, err := ws.ListCollections(ctx)
	if err != nil {
		t.Fatalf("ListCollections: %v", err)
	}
	if len(colls) != 0 {
		t.Errorf("collections after failed creates = %+v, want none", colls)
	}
}
