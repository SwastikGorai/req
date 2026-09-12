package store

import (
	"context"
	"fmt"
	"strings"

	"req/internal/model"
)

// SplitPath splits a slash path into segments. Every segment must be a valid
// name (model.ValidName); empty segments, "."/".." and backslashes make the
// path invalid.
func SplitPath(path string) ([]string, error) {
	segments := strings.Split(path, "/")
	for _, seg := range segments {
		if !model.ValidName(seg) {
			return nil, fmt.Errorf("path %q: invalid segment %q: %w", path, seg, ErrInvalidPath)
		}
	}
	return segments, nil
}

// ResolvedPath is a slash path resolved against stored collections.
// Segments holds all validated path segments (Segments[0] is the collection
// name); Item is the resolved item, or nil when the path stops at the
// collection root.
type ResolvedPath struct {
	Auth       *model.Auth // nearest explicit auth in the resolved hierarchy
	Collection model.Collection
	Rev        Revision
	Segments   []string
	Item       *model.Item
}

// ResolvePath loads the collection named by the first segment (collections
// are identified by their unique Name) and walks Items by Name for each
// remaining segment. A missing collection or a missing named segment returns
// an error wrapping ErrNotFound that names the failing segment; a path that
// continues through a request item (e.g. "Coll/Req/Deeper") returns an error
// (not ErrNotFound) saying a request has no children.
func (w *Workspace) ResolvePath(ctx context.Context, path string) (ResolvedPath, error) {
	segments, err := SplitPath(path)
	if err != nil {
		return ResolvedPath{}, err
	}
	coll, rev, err := w.collectionByName(ctx, segments[0])
	if err != nil {
		return ResolvedPath{}, err
	}
	rp := ResolvedPath{Collection: coll, Rev: rev, Segments: segments}
	rp.Auth = coll.Auth
	children := coll.Items
	for i, seg := range segments[1:] {
		idx := findChild(children, seg)
		if idx < 0 {
			return ResolvedPath{}, fmt.Errorf("no item named %q: %w", seg, ErrNotFound)
		}
		it := children[idx]
		var auth *model.Auth
		if it.Folder != nil {
			auth = it.Folder.Auth
		} else {
			auth = it.Request.Auth
		}
		if auth != nil && auth.Type != "inherit" {
			rp.Auth = auth
		}
		if i == len(segments)-2 { // last segment: this is the resolved item
			rp.Item = &it
			break
		}
		if it.Type != "folder" {
			return ResolvedPath{}, fmt.Errorf("%q is a request and has no children", seg)
		}
		children = it.Folder.Children
	}
	return rp, nil
}

// collectionByName loads the stored collection whose Name equals name,
// scanning the collections directory in filename order so results are
// deterministic. A missing name returns an error wrapping ErrNotFound.
func (w *Workspace) collectionByName(ctx context.Context, name string) (model.Collection, Revision, error) {
	stored, err := w.loadCollections(ctx)
	if err != nil {
		return model.Collection{}, "", err
	}
	for _, s := range stored {
		if s.Collection.Name == name {
			return s.Collection, s.Rev, nil
		}
	}
	return model.Collection{}, "", fmt.Errorf("collection %q: %w", name, ErrNotFound)
}

// findChild returns the index of the item named name (case-sensitive), or
// -1 when absent.
func findChild(items []model.Item, name string) int {
	for i := range items {
		if items[i].Name == name {
			return i
		}
	}
	return -1
}
