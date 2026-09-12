package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCollectionFilenameIdentity(t *testing.T) {
	w := newTestWorkspace(t)
	c, _ := saveFixture(t, w)
	from := filepath.Join(w.Dir(), "collections", c.ID+".json")
	to := filepath.Join(w.Dir(), "collections", "different.json")
	if err := os.Rename(from, to); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(to)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := w.LoadCollection(context.Background(), "different"); err == nil {
		t.Fatal("accepted mismatched filename and ID")
	}
	after, err := os.ReadFile(to)
	if err != nil || string(before) != string(after) {
		t.Fatal("invalid collection changed on load")
	}
}
