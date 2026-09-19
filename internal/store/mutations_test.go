package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/SwastikGorai/req/internal/model"
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

func TestRenameItemAndCollection(t *testing.T) {
	ws := newTestWorkspace(t)
	ctx := context.Background()
	col, err := ws.CreateCollection(ctx, "API")
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	other, err := ws.CreateCollection(ctx, "Other")
	if err != nil {
		t.Fatalf("CreateCollection(Other): %v", err)
	}
	if _, err := ws.CreateFolder(ctx, "API/Auth", false); err != nil {
		t.Fatalf("CreateFolder(Auth): %v", err)
	}
	if err := ws.CreateRequest(ctx, "API/Auth/Login", model.Request{Method: "POST", URL: "{{base_url}}/login"}, false); err != nil {
		t.Fatalf("CreateRequest(Login): %v", err)
	}
	if err := ws.CreateRequest(ctx, "API/Ping", model.Request{Method: "GET", URL: "{{base_url}}/ping"}, false); err != nil {
		t.Fatalf("CreateRequest(Ping): %v", err)
	}

	authID := mustResolve(t, ws, ctx, "API/Auth").Item.ID

	// Renaming the folder keeps parent, position, ID and subtree.
	if err := ws.RenameItem(ctx, "API/Auth", "Security"); err != nil {
		t.Fatalf("RenameItem(API/Auth -> Security): %v", err)
	}
	rp := mustResolve(t, ws, ctx, "API/Security/Login")
	if rp.Item.Request == nil {
		t.Errorf("API/Security/Login resolved to %+v, want the request below the renamed folder", rp.Item)
	}
	if _, err := ws.ResolvePath(ctx, "API/Auth"); !errors.Is(err, ErrNotFound) {
		t.Errorf("old folder path after rename: error = %v, want ErrNotFound", err)
	}
	stored, _, err := ws.LoadCollection(ctx, col.ID)
	if err != nil {
		t.Fatalf("LoadCollection: %v", err)
	}
	if got := itemNames(stored.Items); !reflect.DeepEqual(got, []string{"Security", "Ping"}) {
		t.Errorf("root order after folder rename = %v, want [Security Ping] (siblings intact)", got)
	}
	if stored.Items[0].ID != authID {
		t.Errorf("renamed folder id = %q, want %q", stored.Items[0].ID, authID)
	}

	// Renaming a request works the same way.
	if err := ws.RenameItem(ctx, "API/Ping", "Health"); err != nil {
		t.Fatalf("RenameItem(API/Ping -> Health): %v", err)
	}
	mustResolve(t, ws, ctx, "API/Health")
	if _, err := ws.ResolvePath(ctx, "API/Ping"); !errors.Is(err, ErrNotFound) {
		t.Errorf("old request path after rename: error = %v, want ErrNotFound", err)
	}

	// A sibling already using the new name is a duplicate.
	if err := ws.RenameItem(ctx, "API/Security", "Health"); !errors.Is(err, ErrDuplicateName) {
		t.Errorf("rename onto a sibling name: error = %v, want ErrDuplicateName", err)
	}

	// Invalid names are a plain validation failure.
	for _, name := range []string{"a/b", "", "."} {
		if err := ws.RenameItem(ctx, "API/Security", name); err == nil || errors.Is(err, ErrDuplicateName) {
			t.Errorf("RenameItem to %q: error = %v, want a name validation failure (not ErrDuplicateName)", name, err)
		}
	}

	// A same-name rename writes nothing.
	apiFile := filepath.Join(ws.Dir(), "collections", col.ID+".json")
	before := readFileString(t, apiFile)
	if err := ws.RenameItem(ctx, "API/Security", "Security"); err != nil {
		t.Fatalf("same-name RenameItem: %v", err)
	}
	if after := readFileString(t, apiFile); after != before {
		t.Error("same-name rename rewrote the stored file")
	}

	// RenameCollection changes only the name field; ID and file name stay.
	if err := ws.RenameCollection(ctx, "API", "Renamed API"); err != nil {
		t.Fatalf("RenameCollection(API -> Renamed API): %v", err)
	}
	stored, _, err = ws.LoadCollection(ctx, col.ID)
	if err != nil {
		t.Fatalf("LoadCollection after collection rename: %v", err)
	}
	if stored.Name != "Renamed API" || stored.ID != col.ID {
		t.Errorf("stored collection = id %q name %q, want id %q name %q", stored.ID, stored.Name, col.ID, "Renamed API")
	}
	mustResolve(t, ws, ctx, "Renamed API/Security/Login")
	if _, err := ws.ResolvePath(ctx, "API"); !errors.Is(err, ErrNotFound) {
		t.Errorf("old collection name after rename: error = %v, want ErrNotFound", err)
	}

	// Renaming onto another collection's name is a duplicate and writes
	// nothing to either file.
	otherFile := filepath.Join(ws.Dir(), "collections", other.ID+".json")
	otherBefore := readFileString(t, otherFile)
	before = readFileString(t, apiFile)
	if err := ws.RenameCollection(ctx, "Renamed API", "Other"); !errors.Is(err, ErrDuplicateName) {
		t.Errorf("RenameCollection onto an existing name: error = %v, want ErrDuplicateName", err)
	}
	if after := readFileString(t, apiFile); after != before {
		t.Error("the failed collection rename rewrote the stored file")
	}
	if after := readFileString(t, otherFile); after != otherBefore {
		t.Error("the failed collection rename modified the other collection")
	}

	// A same-name collection rename is a no-op.
	if err := ws.RenameCollection(ctx, "Renamed API", "Renamed API"); err != nil {
		t.Errorf("same-name RenameCollection: %v", err)
	}
}

func TestMoveItem(t *testing.T) {
	ws := newTestWorkspace(t)
	ctx := context.Background()
	col, err := ws.CreateCollection(ctx, "API")
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	if _, err := ws.CreateFolder(ctx, "API/A", false); err != nil {
		t.Fatalf("CreateFolder(A): %v", err)
	}
	if _, err := ws.CreateFolder(ctx, "API/B", false); err != nil {
		t.Fatalf("CreateFolder(B): %v", err)
	}
	if err := ws.CreateRequest(ctx, "API/Health", model.Request{Method: "GET", URL: "{{base_url}}/health"}, false); err != nil {
		t.Fatalf("CreateRequest(Health): %v", err)
	}
	if err := ws.CreateRequest(ctx, "API/A/Nested", model.Request{Method: "POST", URL: "{{base_url}}/nested"}, false); err != nil {
		t.Fatalf("CreateRequest(Nested): %v", err)
	}

	aID := mustResolve(t, ws, ctx, "API/A").Item.ID
	healthID := mustResolve(t, ws, ctx, "API/Health").Item.ID

	// A request becomes the LAST child of the destination folder.
	if err := ws.MoveItem(ctx, "API/Health", "API/A"); err != nil {
		t.Fatalf("MoveItem(API/Health under API/A): %v", err)
	}
	stored, _, err := ws.LoadCollection(ctx, col.ID)
	if err != nil {
		t.Fatalf("LoadCollection: %v", err)
	}
	if got := itemNames(stored.Items); !reflect.DeepEqual(got, []string{"A", "B"}) {
		t.Errorf("root items after request move = %v, want [A B] (Health gone from the old parent)", got)
	}
	if got := itemNames(stored.Items[0].Folder.Children); !reflect.DeepEqual(got, []string{"Nested", "Health"}) {
		t.Errorf("A children after request move = %v, want [Nested Health] (moved item last)", got)
	}

	// A folder moves with its whole subtree and keeps its ID.
	if err := ws.MoveItem(ctx, "API/A", "API/B"); err != nil {
		t.Fatalf("MoveItem(API/A under API/B): %v", err)
	}
	stored, _, err = ws.LoadCollection(ctx, col.ID)
	if err != nil {
		t.Fatalf("LoadCollection after folder move: %v", err)
	}
	if got := itemNames(stored.Items); !reflect.DeepEqual(got, []string{"B"}) {
		t.Errorf("root items after folder move = %v, want [B]", got)
	}
	if got := itemNames(stored.Items[0].Folder.Children); !reflect.DeepEqual(got, []string{"A"}) {
		t.Errorf("B children after folder move = %v, want [A] (moved item last)", got)
	}
	if got := itemNames(stored.Items[0].Folder.Children[0].Folder.Children); !reflect.DeepEqual(got, []string{"Nested", "Health"}) {
		t.Errorf("moved subtree = %v, want [Nested Health] intact", got)
	}
	mustResolve(t, ws, ctx, "API/B/A/Nested")
	if got := mustResolve(t, ws, ctx, "API/B/A/Health").Item.ID; got != healthID {
		t.Errorf("moved request id = %q, want %q", got, healthID)
	}
	if got := mustResolve(t, ws, ctx, "API/B/A").Item.ID; got != aID {
		t.Errorf("moved folder id = %q, want %q", got, aID)
	}
	if _, err := ws.ResolvePath(ctx, "API/A"); !errors.Is(err, ErrNotFound) {
		t.Errorf("old folder path after move: error = %v, want ErrNotFound", err)
	}

	// Cross-collection moves are rejected in both directions.
	if _, err := ws.CreateCollection(ctx, "Other"); err != nil {
		t.Fatalf("CreateCollection(Other): %v", err)
	}
	if err := ws.CreateRequest(ctx, "Other/Solo", model.Request{Method: "GET", URL: "https://other.example.test"}, false); err != nil {
		t.Fatalf("CreateRequest(Solo): %v", err)
	}
	if err := ws.MoveItem(ctx, "Other/Solo", "API"); !errors.Is(err, ErrBadMove) {
		t.Errorf("MoveItem across collections (into API): error = %v, want ErrBadMove", err)
	}
	if err := ws.MoveItem(ctx, "API/B/A/Nested", "Other"); !errors.Is(err, ErrBadMove) {
		t.Errorf("MoveItem across collections (into Other): error = %v, want ErrBadMove", err)
	}

	// destParent naming a request is rejected.
	if err := ws.MoveItem(ctx, "API/B/A/Health", "API/B/A/Nested"); !errors.Is(err, ErrBadMove) {
		t.Errorf("MoveItem under a request: error = %v, want ErrBadMove", err)
	}

	// Missing source and missing destination wrap ErrNotFound.
	if err := ws.MoveItem(ctx, "API/Missing", "API"); !errors.Is(err, ErrNotFound) {
		t.Errorf("MoveItem of a missing item: error = %v, want ErrNotFound", err)
	}
	if err := ws.MoveItem(ctx, "API/B/A/Health", "API/B/Nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("MoveItem to a missing destParent: error = %v, want ErrNotFound", err)
	}

	// A same-named sibling at the destination is a duplicate and nothing
	// is written.
	if err := ws.CreateRequest(ctx, "API/Health", model.Request{Method: "GET", URL: "{{base_url}}/health"}, false); err != nil {
		t.Fatalf("CreateRequest(root Health): %v", err)
	}
	apiFile := filepath.Join(ws.Dir(), "collections", col.ID+".json")
	before := readFileString(t, apiFile)
	if err := ws.MoveItem(ctx, "API/Health", "API/B/A"); !errors.Is(err, ErrDuplicateName) {
		t.Errorf("MoveItem onto a destination sibling name: error = %v, want ErrDuplicateName", err)
	}
	if after := readFileString(t, apiFile); after != before {
		t.Error("the failed move rewrote the stored file")
	}
}

func TestMoveItemCycle(t *testing.T) {
	ws := newTestWorkspace(t)
	ctx := context.Background()
	col, err := ws.CreateCollection(ctx, "API")
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	if _, err := ws.CreateFolder(ctx, "API/A", false); err != nil {
		t.Fatalf("CreateFolder(A): %v", err)
	}
	if _, err := ws.CreateFolder(ctx, "API/A/B", false); err != nil {
		t.Fatalf("CreateFolder(A/B): %v", err)
	}
	apiFile := filepath.Join(ws.Dir(), "collections", col.ID+".json")
	before := readFileString(t, apiFile)

	// A folder cannot move into itself.
	if err := ws.MoveItem(ctx, "API/A", "API/A"); !errors.Is(err, ErrBadMove) {
		t.Errorf("MoveItem(API/A into API/A): error = %v, want ErrBadMove", err)
	}
	// ...nor into one of its descendants.
	if err := ws.MoveItem(ctx, "API/A", "API/A/B"); !errors.Is(err, ErrBadMove) {
		t.Errorf("MoveItem(API/A into API/A/B): error = %v, want ErrBadMove", err)
	}

	// Moving under the current parent is a no-op, not a cycle, and writes
	// nothing.
	if err := ws.MoveItem(ctx, "API/A", "API"); err != nil {
		t.Fatalf("MoveItem(API/A under its own parent API): %v", err)
	}
	if after := readFileString(t, apiFile); after != before {
		t.Error("same-parent move rewrote the stored file")
	}
}

func TestDeleteItemAndCollection(t *testing.T) {
	ws := newTestWorkspace(t)
	ctx := context.Background()
	if _, err := ws.CreateCollection(ctx, "API"); err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	if _, err := ws.CreateFolder(ctx, "API/Auth", false); err != nil {
		t.Fatalf("CreateFolder(Auth): %v", err)
	}
	if err := ws.CreateRequest(ctx, "API/Auth/Login", model.Request{Method: "POST", URL: "{{base_url}}/login"}, false); err != nil {
		t.Fatalf("CreateRequest(Login): %v", err)
	}
	if err := ws.CreateRequest(ctx, "API/Ping", model.Request{Method: "GET", URL: "{{base_url}}/ping"}, false); err != nil {
		t.Fatalf("CreateRequest(Ping): %v", err)
	}

	// The collection root is not an item; deleting it is DeleteCollection.
	if err := ws.DeleteItem(ctx, "API", ""); !errors.Is(err, ErrInvalidPath) {
		t.Errorf("DeleteItem on a single-segment path: error = %v, want ErrInvalidPath", err)
	}
	if err := ws.DeleteItem(ctx, "API/Nope", ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("DeleteItem on a missing item: error = %v, want ErrNotFound", err)
	}
	if err := ws.DeleteItem(ctx, "Nope/Ping", ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("DeleteItem on a missing collection: error = %v, want ErrNotFound", err)
	}

	// Deleting a request removes just it.
	if err := ws.DeleteItem(ctx, "API/Ping", mustResolve(t, ws, ctx, "API/Ping").Rev); err != nil {
		t.Fatalf("DeleteItem(API/Ping): %v", err)
	}
	if _, err := ws.ResolvePath(ctx, "API/Ping"); !errors.Is(err, ErrNotFound) {
		t.Errorf("deleted request still resolves: error = %v, want ErrNotFound", err)
	}
	mustResolve(t, ws, ctx, "API/Auth/Login")

	// Deleting a folder removes its whole subtree.
	if err := ws.DeleteItem(ctx, "API/Auth", mustResolve(t, ws, ctx, "API/Auth").Rev); err != nil {
		t.Fatalf("DeleteItem(API/Auth): %v", err)
	}
	if _, err := ws.ResolvePath(ctx, "API/Auth"); !errors.Is(err, ErrNotFound) {
		t.Errorf("deleted folder still resolves: error = %v, want ErrNotFound", err)
	}
	if _, err := ws.ResolvePath(ctx, "API/Auth/Login"); !errors.Is(err, ErrNotFound) {
		t.Errorf("descendant of a deleted folder still resolves: error = %v, want ErrNotFound", err)
	}

	// DeleteCollection with the fresh revision removes the file.
	ws2 := newTestWorkspace(t)
	temp, err := ws2.CreateCollection(ctx, "Temp")
	if err != nil {
		t.Fatalf("CreateCollection(Temp): %v", err)
	}
	_, rev, err := ws2.LoadCollection(ctx, temp.ID)
	if err != nil {
		t.Fatalf("LoadCollection: %v", err)
	}
	if err := ws2.DeleteCollection(ctx, "Temp", rev); err != nil {
		t.Fatalf("DeleteCollection with the fresh revision: %v", err)
	}
	colls, err := ws2.ListCollections(ctx)
	if err != nil {
		t.Fatalf("ListCollections: %v", err)
	}
	if len(colls) != 0 {
		t.Errorf("collections after delete = %+v, want none", colls)
	}

	// A missing name is ErrNotFound.
	if err := ws2.DeleteCollection(ctx, "Ghost", rev); !errors.Is(err, ErrNotFound) {
		t.Errorf("DeleteCollection on a missing name: error = %v, want ErrNotFound", err)
	}

	// A stale revision conflicts and the file survives. Another writer
	// saves first; the delete then runs against the outdated revision.
	temp2, err := ws2.CreateCollection(ctx, "Temp2")
	if err != nil {
		t.Fatalf("CreateCollection(Temp2): %v", err)
	}
	stored, staleRev, err := ws2.LoadCollection(ctx, temp2.ID)
	if err != nil {
		t.Fatalf("LoadCollection: %v", err)
	}
	stored.Items = append(stored.Items, model.Item{
		Type: "request", ID: "req-extra", Name: "Extra",
		Request: &model.Request{Method: "GET", URL: "https://api.example.test/x"},
	})
	if err := ws2.SaveCollection(ctx, stored, staleRev); err != nil {
		t.Fatalf("concurrent SaveCollection: %v", err)
	}
	err = ws2.DeleteCollection(ctx, "Temp2", staleRev)
	var cerr *ConflictError
	if !errors.As(err, &cerr) {
		t.Fatalf("stale DeleteCollection: got %v, want ConflictError", err)
	}
	if cerr.CollectionID != temp2.ID {
		t.Errorf("conflict CollectionID = %q, want %q", cerr.CollectionID, temp2.ID)
	}
	if _, err := os.Stat(filepath.Join(ws2.Dir(), "collections", temp2.ID+".json")); err != nil {
		t.Errorf("the stale delete removed the collection file: %v", err)
	}
}

func TestUpdateRequest(t *testing.T) {
	ws := newTestWorkspace(t)
	ctx := context.Background()
	col, err := ws.CreateCollection(ctx, "API")
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	if err := ws.CreateRequest(ctx, "API/Ping", model.Request{Method: "GET", URL: "{{base_url}}/ping"}, false); err != nil {
		t.Fatalf("CreateRequest(Ping): %v", err)
	}
	if _, err := ws.CreateFolder(ctx, "API/Auth", false); err != nil {
		t.Fatalf("CreateFolder(Auth): %v", err)
	}
	if err := ws.CreateRequest(ctx, "API/Auth/Login", model.Request{Method: "POST", URL: "{{base_url}}/login"}, false); err != nil {
		t.Fatalf("CreateRequest(Login): %v", err)
	}

	pingID := mustResolve(t, ws, ctx, "API/Ping").Item.ID

	// The update replaces method, URL and headers; ID, name and position
	// stay, and sibling payloads are untouched.
	updated := model.Request{
		Method:  "PUT",
		URL:     "{{base_url}}/ping/{{id}}",
		Headers: []model.Entry{{Key: "X-Trace", Value: "1", Enabled: true}},
	}
	if err := ws.UpdateRequest(ctx, "API/Ping", updated, mustResolve(t, ws, ctx, "API/Ping").Rev); err != nil {
		t.Fatalf("UpdateRequest(API/Ping): %v", err)
	}
	stored, _, err := ws.LoadCollection(ctx, col.ID)
	if err != nil {
		t.Fatalf("LoadCollection: %v", err)
	}
	if got := itemNames(stored.Items); !reflect.DeepEqual(got, []string{"Ping", "Auth"}) {
		t.Errorf("root items after update = %v, want [Ping Auth] (position unchanged)", got)
	}
	ping := stored.Items[0]
	if ping.ID != pingID {
		t.Errorf("updated request id = %q, want %q", ping.ID, pingID)
	}
	if ping.Name != "Ping" {
		t.Errorf("updated request name = %q, want Ping", ping.Name)
	}
	if ping.Request == nil || ping.Request.Method != updated.Method || ping.Request.URL != updated.URL ||
		len(ping.Request.Headers) != 1 || ping.Request.Headers[0].Key != "X-Trace" || !ping.Request.Headers[0].Enabled {
		t.Errorf("updated payload = %+v, want %+v", ping.Request, updated)
	}
	if login := stored.Items[1].Folder.Children[0]; login.Request == nil || login.Request.Method != "POST" {
		t.Errorf("untouched sibling changed: %+v", login)
	}

	// A path naming a folder is a plain error, resolved before any save.
	if err := ws.UpdateRequest(ctx, "API/Auth", updated, mustResolve(t, ws, ctx, "API/Auth").Rev); err == nil {
		t.Error("UpdateRequest on a folder path succeeded, want an error")
	}

	// A payload failing validation is rejected and nothing is written.
	apiFile := filepath.Join(ws.Dir(), "collections", col.ID+".json")
	before := readFileString(t, apiFile)
	if err := ws.UpdateRequest(ctx, "API/Ping", model.Request{Method: "GET", URL: ""}, mustResolve(t, ws, ctx, "API/Ping").Rev); err == nil {
		t.Error("UpdateRequest with an empty URL succeeded, want a validation error")
	}
	if after := readFileString(t, apiFile); after != before {
		t.Error("the rejected update rewrote the stored file")
	}

	// A stale source revision loses via *ConflictError: a competing save
	// lands after the caller captured revision R1, and the caller's update
	// still carrying R1 must not overwrite it.
	_, rev1, err := ws.LoadCollection(ctx, col.ID)
	if err != nil {
		t.Fatalf("LoadCollection: %v", err)
	}
	competing := model.Request{Method: "POST", URL: "{{base_url}}/login-v2"}
	if err := ws.UpdateRequest(ctx, "API/Auth/Login", competing, mustResolve(t, ws, ctx, "API/Auth/Login").Rev); err != nil {
		t.Fatalf("competing UpdateRequest(API/Auth/Login): %v", err)
	}

	err = ws.UpdateRequest(ctx, "API/Ping", model.Request{Method: "PATCH", URL: "{{base_url}}/ping-stale"}, rev1)
	var cerr *ConflictError
	if !errors.As(err, &cerr) {
		t.Fatalf("stale UpdateRequest: got %v, want ConflictError", err)
	}
	if cerr.CollectionID != col.ID {
		t.Errorf("conflict CollectionID = %q, want %q", cerr.CollectionID, col.ID)
	}
	if cerr.Recovery == "" {
		t.Error("conflict error does not point at a recovery file")
	} else if _, statErr := os.Stat(cerr.Recovery); statErr != nil {
		t.Errorf("recovery file missing: %v", statErr)
	}

	// The competitor's content is intact on disk and the stale edit is not.
	winner, _, err := ws.LoadCollection(ctx, col.ID)
	if err != nil {
		t.Fatalf("reload after conflict: %v", err)
	}
	if got := winner.Items[1].Folder.Children[0].Request.URL; got != competing.URL {
		t.Errorf("competing write lost: Login URL = %q, want %q", got, competing.URL)
	}
	if got := winner.Items[0].Request; got == nil || got.Method != "PUT" || got.URL != updated.URL {
		t.Errorf("Ping payload after conflict = %+v, want the previously stored PUT version", got)
	}
}

// mustResolve resolves path or fails the test.
func mustResolve(t *testing.T, ws *Workspace, ctx context.Context, path string) ResolvedPath {
	t.Helper()
	rp, err := ws.ResolvePath(ctx, path)
	if err != nil {
		t.Fatalf("ResolvePath(%q): %v", path, err)
	}
	return rp
}

// itemNames returns the item names in order, for readable assertions.
func itemNames(items []model.Item) []string {
	names := make([]string, len(items))
	for i, it := range items {
		names[i] = it.Name
	}
	return names
}

// readFileString reads a file or fails the test.
func readFileString(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(data)
}
