package store

import (
	"context"
	"errors"
	"github.com/SwastikGorai/req/internal/model"
	"os"
	"path/filepath"
	"testing"
)

func TestEnvironmentStorage(t *testing.T) {
	w := newTestWorkspace(t)
	ctx := context.Background()
	e := model.Environment{SchemaVersion: 1, Name: "local", Variables: map[string]any{"token": "one"}}
	if err := w.SaveEnvironment(ctx, e, ""); err != nil {
		t.Fatal(err)
	}
	loaded, rev, err := w.LoadEnvironment(ctx, "local")
	if err != nil || loaded.Variables["token"] != "one" {
		t.Fatalf("%v %v", loaded, err)
	}
	if envs, err := w.ListEnvironments(ctx); err != nil || len(envs) != 1 {
		t.Fatalf("%v %v", envs, err)
	}
	e.Variables["token"] = "two"
	if err := w.SaveEnvironment(ctx, e, rev); err != nil {
		t.Fatal(err)
	}
	e.Variables["token"] = "loser"
	var conflict *ConflictError
	if err := w.SaveEnvironment(ctx, e, rev); !errors.As(err, &conflict) || conflict.Recovery == "" {
		t.Fatalf("no conflict recovery: %v", err)
	}
	if err := w.DeleteEnvironment(ctx, "local", rev); !errors.As(err, &conflict) {
		t.Fatalf("stale delete: %v", err)
	}
	loaded, rev, err = w.LoadEnvironment(ctx, "local")
	if err != nil || loaded.Variables["token"] != "two" {
		t.Fatalf("%v %v", loaded, err)
	}
	if err := w.DeleteEnvironment(ctx, "local", rev); err != nil {
		t.Fatal(err)
	}
	if _, _, err := w.LoadEnvironment(ctx, "local"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestEnvironmentValidation(t *testing.T) {
	w := newTestWorkspace(t)
	ctx := context.Background()
	for _, name := range []string{"../bad", "CON", "lpt1", "a:b", "a.", "a b", ""} {
		if _, err := w.environmentPath(name); err == nil {
			t.Fatalf("accepted %q", name)
		}
	}
	p := filepath.Join(w.Dir(), "environments", "local.json")
	for _, data := range []string{
		`{"schema_version":1,"name":"local","variables":{}}]`,
		`{"schema_version":2,"name":"local","variables":{}}`,
		`{"schema_version":1,"name":"local","variables":{},"unknown":true}`,
		`{"schema_version":1,"name":"elsewhere","variables":{}}`,
		`{"schema_version":1,"name":"local","variables":{"env:SECRET":"x"}}`,
	} {
		if err := os.WriteFile(p, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if _, _, err := w.LoadEnvironment(ctx, "local"); err == nil {
			t.Fatalf("accepted %s", data)
		}
	}
}
