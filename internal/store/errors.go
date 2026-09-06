package store

import (
	"errors"
	"fmt"
)

// ErrNoWorkspace reports that no workspace could be found or opened.
var ErrNoWorkspace = errors.New("no req workspace found")

// ErrNotFound reports a missing collection file.
var ErrNotFound = errors.New("not found")

// ErrInvalidPath reports a malformed slash path (empty segment, "."/"..", or
// a segment containing a path separator).
var ErrInvalidPath = errors.New("invalid path")

// ErrDuplicateName reports a name collision: a duplicate collection name or
// a duplicate sibling name within one folder.
var ErrDuplicateName = errors.New("duplicate name")

// ErrBadMove reports a move that cannot happen: across collections, into a
// request, or a folder into itself or one of its descendants.
var ErrBadMove = errors.New("invalid move")

// ConflictError reports a failed revision check: the file on disk changed
// since the caller loaded it. The rejected candidate content is preserved
// in Recovery (when it could be written) instead of overwriting blindly.
type ConflictError struct {
	CollectionID string
	Have         string // revision found on disk ("" if the file vanished)
	Want         string // revision the caller expected ("" means expected absence)
	Recovery     string // path of the preserved candidate, if any
}

func (e *ConflictError) Error() string {
	switch {
	case e.Have == "":
		return fmt.Sprintf("collection %q changed on disk while it was being edited (expected revision %s no longer exists)", e.CollectionID, shortRev(e.Want))
	case e.Want == "":
		return fmt.Sprintf("collection %q already exists (load it before overwriting)", e.CollectionID)
	default:
		return fmt.Sprintf("collection %q changed on disk (have revision %s, expected %s)", e.CollectionID, shortRev(e.Have), shortRev(e.Want))
	}
}

func shortRev(r string) string {
	const n = 12
	if len(r) > n {
		return r[:n]
	}
	return r
}
