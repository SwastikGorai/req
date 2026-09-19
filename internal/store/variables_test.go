package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/SwastikGorai/req/internal/model"
)

func TestPersistVariables(t *testing.T) {
	ws := newTestWorkspace(t)
	ctx := context.Background()
	c := model.Collection{SchemaVersion: model.SchemaVersion, ID: "col-vars", Name: "API", Variables: map[string]any{"old": "collection"}}
	if err := ws.SaveCollection(ctx, c, ""); err != nil {
		t.Fatal(err)
	}
	e := model.Environment{SchemaVersion: model.SchemaVersion, Name: "local", Variables: map[string]any{"token": "old"}}
	if err := ws.SaveEnvironment(ctx, e, ""); err != nil {
		t.Fatal(err)
	}
	_, collectionRevision, err := ws.LoadCollection(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, environmentRevision, err := ws.LoadEnvironment(ctx, e.Name)
	if err != nil {
		t.Fatal(err)
	}
	if err := ws.PersistVariables(ctx, VariableChanges{
		CollectionName: c.Name, CollectionRevision: collectionRevision,
		Collection:      map[string]VariableChange{"new": {Value: true}},
		EnvironmentName: e.Name, EnvironmentRevision: environmentRevision,
		Environment: map[string]VariableChange{"token": {Value: "fresh"}},
	}); err != nil {
		t.Fatal(err)
	}
	loadedCollection, _, err := ws.LoadCollection(ctx, c.ID)
	if err != nil || loadedCollection.Variables["new"] != true {
		t.Fatalf("collection = %#v, err = %v", loadedCollection.Variables, err)
	}
	loadedEnvironment, _, err := ws.LoadEnvironment(ctx, e.Name)
	if err != nil || loadedEnvironment.Variables["token"] != "fresh" {
		t.Fatalf("environment = %#v, err = %v", loadedEnvironment.Variables, err)
	}
	for _, path := range []string{
		filepath.Join(ws.Dir(), "collections", c.ID+".json"),
		filepath.Join(ws.Dir(), "environments", e.Name+".json"),
	} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.HasSuffix(data, []byte{'\n'}) || bytes.HasSuffix(data, []byte("\n\n")) {
			t.Fatalf("%s has incorrect trailing newlines: %q", path, data[len(data)-min(4, len(data)):])
		}
	}
}

func TestJournalRecovery(t *testing.T) {
	ws := newTestWorkspace(t)
	ctx := context.Background()
	c := model.Collection{SchemaVersion: model.SchemaVersion, ID: "col-journal", Name: "API", Variables: map[string]any{"token": "old"}}
	if err := ws.SaveCollection(ctx, c, ""); err != nil {
		t.Fatal(err)
	}
	e := model.Environment{SchemaVersion: model.SchemaVersion, Name: "local", Variables: map[string]any{"token": "old"}}
	if err := ws.SaveEnvironment(ctx, e, ""); err != nil {
		t.Fatal(err)
	}
	collectionPath := filepath.Join(ws.Dir(), "collections", c.ID+".json")
	environmentPath := filepath.Join(ws.Dir(), "environments", e.Name+".json")
	collectionBefore, err := os.ReadFile(collectionPath)
	if err != nil {
		t.Fatal(err)
	}
	environmentBefore, err := os.ReadFile(environmentPath)
	if err != nil {
		t.Fatal(err)
	}
	c.Variables["token"] = "new"
	e.Variables["token"] = "new"
	collectionAfter, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	collectionAfter = append(collectionAfter, '\n')
	environmentAfter, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	environmentAfter = append(environmentAfter, '\n')
	if err := writeVariableJournal(ws.Root(), variableJournal{Version: 1, Files: []variableJournalEntry{
		{Path: filepath.Join("collections", c.ID+".json"), Before: collectionBefore, After: collectionAfter, Mode: 0o644},
		{Path: filepath.Join("environments", e.Name+".json"), Before: environmentBefore, After: environmentAfter, Mode: 0o600},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(collectionPath, collectionAfter, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(ws.Root()); err != nil {
		t.Fatalf("Open recovery: %v", err)
	}
	if got, err := os.ReadFile(collectionPath); err != nil || string(got) != string(collectionBefore) {
		t.Fatalf("collection after recovery = %q, err = %v", got, err)
	}
	if got, err := os.ReadFile(environmentPath); err != nil || string(got) != string(environmentBefore) {
		t.Fatalf("environment after recovery = %q, err = %v", got, err)
	}
	if _, err := os.Stat(variableJournalPath(ws.Root())); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("journal still exists: %v", err)
	}
}

func TestRecoveryAcquiresWorkspaceLock(t *testing.T) {
	ws := newTestWorkspace(t)
	c := model.Collection{SchemaVersion: model.SchemaVersion, ID: "col-lock", Name: "API"}
	if err := ws.SaveCollection(context.Background(), c, ""); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(ws.Dir(), "collections", c.ID+".json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeVariableJournal(ws.Root(), variableJournal{Version: 1, Files: []variableJournalEntry{{
		Path: filepath.Join("collections", c.ID+".json"), Before: before, After: before, Mode: 0o644,
	}}}); err != nil {
		t.Fatal(err)
	}
	unlock, err := ws.lock(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = recoverVariableJournal(ctx, ws.Root())
	unlock()
	if !errors.Is(err, ErrRecovery) {
		t.Fatalf("recovery while lock held = %v, want an actionable lock error", err)
	}
	if _, err := os.Stat(variableJournalPath(ws.Root())); err != nil {
		t.Fatalf("journal disappeared while lock was held: %v", err)
	}
	if _, err := Open(ws.Root()); err != nil {
		t.Fatalf("recovery after releasing lock: %v", err)
	}
}

func TestPersistVariablesConflictPreservesCandidate(t *testing.T) {
	ws := newTestWorkspace(t)
	ctx := context.Background()
	e := model.Environment{SchemaVersion: model.SchemaVersion, Name: "local", Variables: map[string]any{"token": "old"}}
	if err := ws.SaveEnvironment(ctx, e, ""); err != nil {
		t.Fatal(err)
	}
	_, stale, err := ws.LoadEnvironment(ctx, e.Name)
	if err != nil {
		t.Fatal(err)
	}
	e.Variables["other"] = "winner"
	if err := ws.SaveEnvironment(ctx, e, stale); err != nil {
		t.Fatal(err)
	}
	var conflict *ConflictError
	err = ws.PersistVariables(ctx, VariableChanges{
		EnvironmentName: e.Name, EnvironmentRevision: stale,
		Environment: map[string]VariableChange{"token": {Value: "stale"}},
	})
	if !errors.As(err, &conflict) || conflict.Recovery == "" {
		t.Fatalf("error = %v, want a conflict recovery", err)
	}
	loaded, _, err := ws.LoadEnvironment(ctx, e.Name)
	if err != nil || loaded.Variables["other"] != "winner" || loaded.Variables["token"] != "old" {
		t.Fatalf("environment after conflict = %#v, err = %v", loaded.Variables, err)
	}
}
