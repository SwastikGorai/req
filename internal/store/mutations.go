package store

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"

	"req/internal/model"
)

// CreateFolder creates the folder at path (at least two segments: the
// collection name plus at least one folder name). Missing intermediate
// folders are an error unless parents is true, which creates them; parents
// never creates the collection itself (a missing collection is always
// ErrNotFound). It returns the names of the folders actually created,
// innermost last. An existing leaf or same-named sibling at any insertion
// point returns an error wrapping ErrDuplicateName.
func (w *Workspace) CreateFolder(ctx context.Context, path string, parents bool) ([]string, error) {
	segments, err := splitForCreate(path, "Collection/Folder")
	if err != nil {
		return nil, err
	}
	return w.createItem(ctx, segments, parents, func(name string) model.Item {
		return model.Item{Type: "folder", ID: model.NewID("fld"), Name: name, Folder: &model.Folder{}}
	})
}

// CreateRequest stores req under the folder path prefix of path (the last
// segment is the request name; the collection root is allowed, i.e.
// "Coll/Ping"). parents behaves as in CreateFolder. req must carry a valid
// method/URL — SaveCollection's validation enforces this, but model basics
// are pre-checked so errors name the request. An existing leaf or same-named
// sibling returns an error wrapping ErrDuplicateName.
func (w *Workspace) CreateRequest(ctx context.Context, path string, req model.Request, parents bool) error {
	segments, err := splitForCreate(path, "Collection/Request")
	if err != nil {
		return err
	}
	name := segments[len(segments)-1]
	if req.Method == "" {
		return fmt.Errorf("request %q: empty method", name)
	}
	if req.URL == "" {
		return fmt.Errorf("request %q: empty URL", name)
	}
	_, err = w.createItem(ctx, segments, parents, func(leaf string) model.Item {
		stored := req
		return model.Item{Type: "request", ID: model.NewID("req"), Name: leaf, Request: &stored}
	})
	return err
}

// splitForCreate splits path and requires at least two segments: a
// collection name plus at least one item name. want names the expected
// shape in the error message.
func splitForCreate(path, want string) ([]string, error) {
	segments, err := SplitPath(path)
	if err != nil {
		return nil, err
	}
	if len(segments) < 2 {
		return nil, fmt.Errorf("needs a path like %q — %q names no item: %w", want, path, ErrInvalidPath)
	}
	return segments, nil
}

// createItem resolves the collection named by segments[0] and appends the
// item built by newLeaf at segments[1:], walking existing folders by name.
// Missing intermediate folders are an error unless parents is true, which
// creates them. The final leaf must not already exist. It persists the
// collection with the revision captured at load and returns the names of
// the items it created, innermost last.
func (w *Workspace) createItem(ctx context.Context, segments []string, parents bool, newLeaf func(name string) model.Item) ([]string, error) {
	coll, rev, err := w.collectionByName(ctx, segments[0])
	if err != nil {
		return nil, err
	}
	var created []string
	rest := segments[1:]
	children := &coll.Items
	for i, seg := range rest {
		last := i == len(rest)-1
		idx := findChild(*children, seg)
		if idx >= 0 {
			existing := (*children)[idx]
			if last {
				return nil, fmt.Errorf("%q already exists as a %s: %w", seg, existing.Type, ErrDuplicateName)
			}
			if existing.Type != "folder" {
				return nil, fmt.Errorf("%q is a request and has no children", seg)
			}
			children = &(*children)[idx].Folder.Children
			continue
		}
		switch {
		case last:
			*children = append(*children, newLeaf(seg))
			created = append(created, seg)
		case parents:
			*children = append(*children, model.Item{Type: "folder", ID: model.NewID("fld"), Name: seg, Folder: &model.Folder{}})
			created = append(created, seg)
			children = &(*children)[len(*children)-1].Folder.Children
		default:
			return nil, fmt.Errorf("no folder named %q: %w", seg, ErrNotFound)
		}
	}
	if err := w.SaveCollection(ctx, coll, rev); err != nil {
		return nil, fmt.Errorf("saving collection %q: %w", coll.Name, err)
	}
	return created, nil
}

// RenameCollection renames the stored collection named oldName to newName
// (the file name and ID stay; only the JSON name field changes). newName
// equal to the current name is a no-op returning nil without writing. A
// name used by another collection wraps ErrDuplicateName.
func (w *Workspace) RenameCollection(ctx context.Context, oldName, newName string) error {
	coll, rev, err := w.collectionByName(ctx, oldName)
	if err != nil {
		return err
	}
	if newName == oldName {
		return nil
	}
	if !model.ValidName(newName) {
		return fmt.Errorf("collection name %q is invalid (names must be non-empty, must not be %q or %q, and must contain no / or \\)", newName, ".", "..")
	}
	coll.Name = newName
	if err := w.SaveCollection(ctx, coll, rev); err != nil {
		return fmt.Errorf("saving collection %q: %w", coll.Name, err)
	}
	return nil
}

// DeleteCollection removes the collection file after checking, under the
// workspace lock, that its current revision still equals expected. A stale
// or vanished file returns *ConflictError and nothing is deleted; a delete
// has no candidate content, so the conflict carries no recovery file.
func (w *Workspace) DeleteCollection(ctx context.Context, name string, expected Revision) error {
	coll, _, err := w.collectionByName(ctx, name)
	if err != nil {
		return err
	}
	path, err := w.collectionPath(coll.ID)
	if err != nil {
		return err
	}

	unlock, err := w.lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()

	current, err := os.ReadFile(path)
	switch {
	case err == nil:
		if have := Revision(hashBytes(current)); have != expected {
			return &ConflictError{CollectionID: coll.ID, Have: string(have), Want: string(expected)}
		}
	case errors.Is(err, fs.ErrNotExist):
		return &ConflictError{CollectionID: coll.ID, Have: "", Want: string(expected)}
	default:
		return fmt.Errorf("collection %q: %w", coll.ID, err)
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("removing collection %q: %w", name, err)
	}
	return nil
}

// RenameItem renames the folder or request at path (≥2 segments) to
// newName, in place: same parent, same ID, same position. newName equal to
// the current name is a no-op returning nil without writing. A sibling
// already named newName wraps ErrDuplicateName.
func (w *Workspace) RenameItem(ctx context.Context, path, newName string) error {
	segments, err := splitForCreate(path, "Collection/Item")
	if err != nil {
		return err
	}
	coll, rev, err := w.collectionByName(ctx, segments[0])
	if err != nil {
		return err
	}
	parent, idx, err := locateItem(&coll, segments[1:])
	if err != nil {
		return err
	}
	if (*parent)[idx].Name == newName {
		return nil
	}
	if !model.ValidName(newName) {
		return fmt.Errorf("item name %q is invalid (names must be non-empty, must not be %q or %q, and must contain no / or \\)", newName, ".", "..")
	}
	if findChild(*parent, newName) >= 0 {
		return fmt.Errorf("sibling named %q already exists: %w", newName, ErrDuplicateName)
	}
	(*parent)[idx].Name = newName
	if err := w.SaveCollection(ctx, coll, rev); err != nil {
		return fmt.Errorf("saving collection %q: %w", coll.Name, err)
	}
	return nil
}

// MoveItem moves the folder or request at src (≥2 segments) under
// destParent, keeping its name and ID; it becomes the LAST child of
// destParent. destParent is either a single segment (the collection root)
// or a path to an existing folder. Moving under the item's current parent
// is a no-op returning nil without writing. A missing src item or a missing
// destParent wraps ErrNotFound; destParent naming a request, src and
// destParent in different collections, and a folder moving into itself or
// one of its descendants wrap ErrBadMove; a sibling of destParent already
// using the moved item's name wraps ErrDuplicateName.
func (w *Workspace) MoveItem(ctx context.Context, src, destParent string) error {
	srcSegments, err := splitForCreate(src, "Collection/Item")
	if err != nil {
		return err
	}
	destSegments, err := SplitPath(destParent)
	if err != nil {
		return err
	}
	if destSegments[0] != srcSegments[0] {
		return fmt.Errorf("cannot move %q into another collection (%q): %w", src, destParent, ErrBadMove)
	}
	coll, rev, err := w.collectionByName(ctx, srcSegments[0])
	if err != nil {
		return err
	}
	srcRest := srcSegments[1:]
	destRest := destSegments[1:]
	parent, idx, err := locateItem(&coll, srcRest)
	if err != nil {
		return err
	}
	// A move under the current parent changes nothing — and is not a
	// cycle, so it is checked first.
	if slices.Equal(destRest, srcRest[:len(srcRest)-1]) {
		return nil
	}
	// A folder cannot move into itself or a descendant: the destination
	// path would contain the moved item.
	if len(destRest) >= len(srcRest) && slices.Equal(destRest[:len(srcRest)], srcRest) {
		return fmt.Errorf("cannot move %q into itself or one of its descendants (%q): %w", src, destParent, ErrBadMove)
	}
	dest, err := resolveFolderChildren(&coll, destRest)
	if err != nil {
		return err
	}
	item := (*parent)[idx]
	if findChild(*dest, item.Name) >= 0 {
		return fmt.Errorf("sibling named %q already exists in %q: %w", item.Name, destParent, ErrDuplicateName)
	}
	*parent = append((*parent)[:idx], (*parent)[idx+1:]...)
	*dest = append(*dest, item)
	if err := w.SaveCollection(ctx, coll, rev); err != nil {
		return fmt.Errorf("saving collection %q: %w", coll.Name, err)
	}
	return nil
}

// DeleteItem removes the folder (with its whole subtree) or the request at
// path (≥2 segments). A missing item wraps ErrNotFound. The collection root
// is not an item: deleting a collection is DeleteCollection, so a
// single-segment path here wraps ErrInvalidPath with a pointer there.
// expected is the revision captured before confirmation or other user interaction.
func (w *Workspace) DeleteItem(ctx context.Context, path string, expected Revision) error {
	segments, err := SplitPath(path)
	if err != nil {
		return err
	}
	if len(segments) < 2 {
		return fmt.Errorf("needs a path like %q — %q names no item (deleting a collection is DeleteCollection): %w", "Collection/Item", path, ErrInvalidPath)
	}
	coll, _, err := w.collectionByName(ctx, segments[0])
	if err != nil {
		return err
	}
	parent, idx, err := locateItem(&coll, segments[1:])
	if err != nil {
		return err
	}
	*parent = append((*parent)[:idx], (*parent)[idx+1:]...)
	if err := w.SaveCollection(ctx, coll, expected); err != nil {
		return fmt.Errorf("saving collection %q: %w", coll.Name, err)
	}
	return nil
}

// UpdateRequest replaces the request payload of the request item at path
// with req (the item's ID, name and position are untouched), persisting
// ONLY if the collection file still has the expected revision — the check
// happens under the workspace lock inside SaveCollection, so an edit that
// started from a stale revision loses via *ConflictError (candidate
// preserved in recovery) instead of overwriting a newer save. A missing
// item wraps ErrNotFound; a path naming a folder returns a plain error.
func (w *Workspace) UpdateRequest(ctx context.Context, path string, req model.Request, expected Revision) error {
	segments, err := splitForCreate(path, "Collection/Request")
	if err != nil {
		return err
	}
	coll, _, err := w.collectionByName(ctx, segments[0])
	if err != nil {
		return err
	}
	parent, idx, err := locateItem(&coll, segments[1:])
	if err != nil {
		return err
	}
	if (*parent)[idx].Type != "request" {
		return fmt.Errorf("%q is a folder, not a request", path)
	}
	(*parent)[idx].Request = &req
	if err := w.SaveCollection(ctx, coll, expected); err != nil {
		return fmt.Errorf("saving collection %q: %w", coll.Name, err)
	}
	return nil
}

// locateItem walks the item segments below the collection name against coll
// and returns the children slice holding the item named by the last segment
// plus its index in it. A missing name returns an error wrapping
// ErrNotFound; descending through a request returns a plain error.
func locateItem(coll *model.Collection, segments []string) (*[]model.Item, int, error) {
	if len(segments) == 0 {
		return nil, 0, fmt.Errorf("path names no item: %w", ErrInvalidPath)
	}
	children := &coll.Items
	for _, seg := range segments[:len(segments)-1] {
		idx := findChild(*children, seg)
		if idx < 0 {
			return nil, 0, fmt.Errorf("no item named %q: %w", seg, ErrNotFound)
		}
		it := &(*children)[idx]
		if it.Type != "folder" {
			return nil, 0, fmt.Errorf("%q is a request and has no children", seg)
		}
		children = &it.Folder.Children
	}
	name := segments[len(segments)-1]
	idx := findChild(*children, name)
	if idx < 0 {
		return nil, 0, fmt.Errorf("no item named %q: %w", name, ErrNotFound)
	}
	return children, idx, nil
}

// resolveFolderChildren resolves destParent segments (below the collection
// name) against coll and returns the children slice of the folder they
// name; empty segments select the collection root. A missing folder wraps
// ErrNotFound; a request on the way wraps ErrBadMove, since nothing can be
// moved under a request.
func resolveFolderChildren(coll *model.Collection, segments []string) (*[]model.Item, error) {
	children := &coll.Items
	for _, seg := range segments {
		idx := findChild(*children, seg)
		if idx < 0 {
			return nil, fmt.Errorf("no folder named %q: %w", seg, ErrNotFound)
		}
		it := &(*children)[idx]
		if it.Type != "folder" {
			return nil, fmt.Errorf("cannot move under %q: it is a request: %w", seg, ErrBadMove)
		}
		children = &it.Folder.Children
	}
	return children, nil
}
