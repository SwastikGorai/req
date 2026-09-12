package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"

	"req/internal/model"
)

// Revision is the opaque content hash of one stored file. LoadCollection
// returns it; SaveCollection requires it back and refuses to overwrite a
// file whose current revision differs.
type Revision string

// LoadCollection reads, strictly decodes and validates one collection file.
// The returned Revision hashes the exact file bytes.
func (w *Workspace) LoadCollection(_ context.Context, id string) (model.Collection, Revision, error) {
	path, err := w.collectionPath(id)
	if err != nil {
		return model.Collection{}, "", err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return model.Collection{}, "", fmt.Errorf("collection %q: %w", id, ErrNotFound)
	}
	if err != nil {
		return model.Collection{}, "", fmt.Errorf("collection %q: %w", id, err)
	}
	var c model.Collection
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return model.Collection{}, "", fmt.Errorf("collection %q: invalid JSON: %w", id, err)
	}
	if dec.Decode(new(any)) != io.EOF {
		return model.Collection{}, "", fmt.Errorf("collection %q: trailing data after the JSON document", id)
	}
	if err := c.Validate(); err != nil {
		return model.Collection{}, "", fmt.Errorf("collection %q: %w", id, err)
	}
	if c.ID != id {
		return model.Collection{}, "", fmt.Errorf("collection id %q does not match filename %q", c.ID, id)
	}
	return c, Revision(hashBytes(data)), nil
}

// SaveCollection validates c and atomically replaces its file, but only if
// the file's current revision equals expected. The whole check-and-replace
// runs under the workspace mutation lock, so concurrent writers cannot race
// past the revision check. On conflict the candidate is preserved in a
// recovery file and the stored file is left untouched.
func (w *Workspace) SaveCollection(ctx context.Context, c model.Collection, expected Revision) error {
	if err := c.Validate(); err != nil {
		return fmt.Errorf("collection %q: %w", c.ID, err)
	}
	path, err := w.collectionPath(c.ID)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	unlock, err := w.lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()

	current, err := os.ReadFile(path)
	switch {
	case err == nil:
		if have := Revision(hashBytes(current)); expected == "" || have != expected {
			return w.conflict(c.ID, data, string(have), string(expected))
		}
	case errors.Is(err, fs.ErrNotExist):
		if expected != "" {
			return w.conflict(c.ID, data, "", string(expected))
		}
	default:
		return fmt.Errorf("collection %q: %w", c.ID, err)
	}
	collections, err := w.ListCollections(ctx)
	if err != nil {
		return err
	}
	for _, existing := range collections {
		if existing.ID != c.ID && existing.Name == c.Name {
			return fmt.Errorf("collection %q already exists: %w", c.Name, ErrDuplicateName)
		}
	}
	return writeFileAtomic(path, data, 0o644)
}

func (w *Workspace) collectionPath(id string) (string, error) {
	if !model.ValidID(id) {
		return "", fmt.Errorf("collection id %q is invalid", id)
	}
	return filepath.Join(w.Dir(), "collections", id+".json"), nil
}

// lock acquires the workspace mutation lock, covering the revision reread
// and the atomic replace. It honors ctx cancellation while waiting.
func (w *Workspace) lock(ctx context.Context) (func(), error) {
	l := flock.New(filepath.Join(w.Dir(), ".lock"))
	ok, err := l.TryLockContext(ctx, 50*time.Millisecond)
	if err == nil && ok {
		return func() { _ = l.Unlock() }, nil
	}
	if cerr := ctx.Err(); cerr != nil {
		return nil, fmt.Errorf("acquiring workspace lock: %w", cerr)
	}
	return nil, fmt.Errorf("acquiring workspace lock: %w", err)
}

// SaveRecovery preserves data under .req/recovery/<id>-<shorthash>.json
// (0600) and returns its path. It is used for rejected save candidates and
// for failed editor edits; a missing recovery directory skips the write and
// returns an empty path.
func (w *Workspace) SaveRecovery(id string, data []byte) (string, error) {
	recoveryDir := filepath.Join(w.Dir(), "recovery")
	if !dirExists(recoveryDir) {
		return "", nil
	}
	recovery := filepath.Join(recoveryDir, fmt.Sprintf("%s-%s.json", id, shortRev(hashBytes(data))))
	if err := writeFileAtomic(recovery, data, 0o600); err != nil {
		return "", err
	}
	return recovery, nil
}

// conflict preserves the rejected candidate under .req/recovery and returns
// a ConflictError pointing at it.
func (w *Workspace) conflict(id string, candidate []byte, have, want string) error {
	cerr := &ConflictError{CollectionID: id, Have: have, Want: want}
	if recovery, err := w.SaveRecovery(id, candidate); err == nil {
		cerr.Recovery = recovery
	}
	return cerr
}

// writeFileAtomic writes data through a temporary file in the same
// directory (flushed and closed) and atomically replaces path. A failed
// attempt never leaves a partial destination.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-")
	if err != nil {
		return fmt.Errorf("creating temporary file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	defer func() {
		if tmpName != "" {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replacing %s: %w", path, err)
	}
	tmpName = "" // renamed into place; nothing to clean up
	return nil
}

func hashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
