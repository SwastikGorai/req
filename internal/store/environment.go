package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/SwastikGorai/req/internal/model"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func (w *Workspace) environmentPath(name string) (string, error) {
	if !model.ValidEnvironmentName(name) {
		return "", fmt.Errorf("environment %q: %w", name, ErrInvalidPath)
	}
	return filepath.Join(w.Dir(), "environments", name+".json"), nil
}

func (w *Workspace) LoadEnvironment(_ context.Context, name string) (model.Environment, Revision, error) {
	var e model.Environment
	p, err := w.environmentPath(name)
	if err != nil {
		return e, "", err
	}
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return e, "", fmt.Errorf("environment %q: %w", name, ErrNotFound)
	}
	if err != nil {
		return e, "", err
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(&e); err != nil {
		return e, "", err
	}
	if d.Decode(new(any)) != io.EOF {
		return e, "", fmt.Errorf("trailing data in environment %q", name)
	}
	if err := e.Validate(); err != nil {
		return e, "", err
	}
	if e.Name != name {
		return e, "", fmt.Errorf("environment name does not match filename %q", name)
	}
	return e, Revision(hashBytes(b)), nil
}

func (w *Workspace) SaveEnvironment(ctx context.Context, e model.Environment, expected Revision) error {
	if err := e.Validate(); err != nil {
		return err
	}
	p, err := w.environmentPath(e.Name)
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	unlock, err := w.lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	current, err := os.ReadFile(p)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	var have Revision
	if err == nil {
		have = Revision(hashBytes(current))
	}
	if have != expected {
		return w.conflict("env-"+e.Name, b, string(have), string(expected))
	}
	return writeFileAtomic(p, b, 0600)
}

func (w *Workspace) ListEnvironments(ctx context.Context) ([]model.Environment, error) {
	entries, err := os.ReadDir(filepath.Join(w.Dir(), "environments"))
	if err != nil {
		return nil, err
	}
	result := []model.Environment{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		e, _, err := w.LoadEnvironment(ctx, strings.TrimSuffix(entry.Name(), ".json"))
		if err != nil {
			return nil, err
		}
		result = append(result, e)
	}
	return result, nil
}

func (w *Workspace) DeleteEnvironment(ctx context.Context, name string, expected Revision) error {
	p, err := w.environmentPath(name)
	if err != nil {
		return err
	}
	unlock, err := w.lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	_, have, err := w.LoadEnvironment(ctx, name)
	if errors.Is(err, ErrNotFound) && expected != "" {
		return &ConflictError{CollectionID: "env-" + name, Want: string(expected)}
	}
	if err != nil {
		return err
	}
	if have != expected {
		return &ConflictError{CollectionID: "env-" + name, Have: string(have), Want: string(expected)}
	}
	return os.Remove(p)
}
