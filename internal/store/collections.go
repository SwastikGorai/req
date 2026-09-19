package store

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/SwastikGorai/req/internal/model"
)

// storedCollection pairs a loaded collection with the revision of its file.
type storedCollection struct {
	Collection model.Collection
	Rev        Revision
}

// loadCollections loads every *.json file in the collections directory, in
// filename order. A directory listing error or any file that fails to load
// fails the call (no silent skips); non-.json entries are skipped
// defensively.
func (w *Workspace) loadCollections(ctx context.Context) ([]storedCollection, error) {
	dir := filepath.Join(w.Dir(), "collections")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", dir, err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	stored := make([]storedCollection, 0, len(names))
	for _, name := range names {
		c, rev, err := w.LoadCollection(ctx, strings.TrimSuffix(name, ".json"))
		if err != nil {
			return nil, err
		}
		stored = append(stored, storedCollection{Collection: c, Rev: rev})
	}
	return stored, nil
}

// ListCollections loads every stored collection, sorted by Name.
// A directory listing error or any file that fails to load fails the call
// (no silent skips).
func (w *Workspace) ListCollections(ctx context.Context) ([]model.Collection, error) {
	stored, err := w.loadCollections(ctx)
	if err != nil {
		return nil, err
	}
	sort.Slice(stored, func(i, j int) bool {
		return stored[i].Collection.Name < stored[j].Collection.Name
	})
	out := make([]model.Collection, len(stored))
	for i, s := range stored {
		out[i] = s.Collection
	}
	return out, nil
}

// CreateCollection stores a new empty collection with the given name and a
// fresh NewID("col") id. A duplicate name among stored collections returns
// an error wrapping ErrDuplicateName.
func (w *Workspace) CreateCollection(ctx context.Context, name string) (model.Collection, error) {
	if !model.ValidName(name) {
		return model.Collection{}, fmt.Errorf("collection name %q is invalid (names must be non-empty, must not be %q or %q, and must contain no / or \\)", name, ".", "..")
	}
	c := model.Collection{
		SchemaVersion: model.SchemaVersion,
		ID:            model.NewID("col"),
		Name:          name,
		Items:         []model.Item{},
	}
	if err := w.SaveCollection(ctx, c, ""); err != nil {
		return model.Collection{}, err
	}
	return c, nil
}
