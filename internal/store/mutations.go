package store

import (
	"context"
	"fmt"

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
