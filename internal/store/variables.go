package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/SwastikGorai/req/internal/model"
)

const variableJournalName = "variables-journal.json"

// VariableChange is one script mutation to a persisted variable layer.
type VariableChange struct {
	Value any
	Unset bool
}

// VariableChanges is the revision-checked set of collection/environment
// mutations produced by one saved request run.
type VariableChanges struct {
	CollectionName      string
	CollectionRevision  Revision
	Collection          map[string]VariableChange
	EnvironmentName     string
	EnvironmentRevision Revision
	Environment         map[string]VariableChange
}

type variableJournal struct {
	Version int                    `json:"version"`
	Files   []variableJournalEntry `json:"files"`
}

type variableJournalEntry struct {
	Path   string `json:"path"`
	Before []byte `json:"before"`
	After  []byte `json:"after"`
	Mode   uint32 `json:"mode"`
}

// PersistVariables applies the dirty collection/environment overlays under
// one workspace lock. A journal remains until every target has been replaced.
func (w *Workspace) PersistVariables(ctx context.Context, changes VariableChanges) (err error) {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if len(changes.Collection) == 0 && len(changes.Environment) == 0 {
		return nil
	}
	if len(changes.Collection) > 0 && changes.CollectionName == "" {
		return fmt.Errorf("persisting collection variables requires a collection")
	}
	if len(changes.Environment) > 0 && changes.EnvironmentName == "" {
		return fmt.Errorf("persisting environment variables requires --env")
	}

	unlock, err := w.lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	if err := recoverVariableJournalLocked(w.root); err != nil {
		return err
	}

	targets := make([]variableJournalEntry, 0, 2)
	if len(changes.Collection) > 0 {
		collection, have, err := w.collectionByName(ctx, changes.CollectionName)
		if err != nil {
			return err
		}
		if have != changes.CollectionRevision {
			if candidate, candidateErr := marshalCollectionVariables(collection, changes.Collection); candidateErr == nil {
				return w.conflict(collection.ID, candidate, string(have), string(changes.CollectionRevision))
			}
			return &ConflictError{CollectionID: collection.ID, Have: string(have), Want: string(changes.CollectionRevision)}
		}
		path, err := w.collectionPath(collection.ID)
		if err != nil {
			return err
		}
		before, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading collection %q: %w", changes.CollectionName, err)
		}
		after, err := marshalCollectionVariables(collection, changes.Collection)
		if err != nil {
			return fmt.Errorf("collection %q: %w", changes.CollectionName, err)
		}
		targets = append(targets, variableJournalEntry{
			Path: filepath.Join("collections", collection.ID+".json"), Before: before,
			After: after, Mode: 0o644,
		})
	}

	if len(changes.Environment) > 0 {
		environment, have, err := w.LoadEnvironment(ctx, changes.EnvironmentName)
		if err != nil {
			return err
		}
		if have != changes.EnvironmentRevision {
			if candidate, candidateErr := marshalEnvironmentVariables(environment, changes.Environment); candidateErr == nil {
				return w.conflict("env-"+changes.EnvironmentName, candidate, string(have), string(changes.EnvironmentRevision))
			}
			return &ConflictError{CollectionID: "env-" + changes.EnvironmentName, Have: string(have), Want: string(changes.EnvironmentRevision)}
		}
		path, err := w.environmentPath(changes.EnvironmentName)
		if err != nil {
			return err
		}
		before, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading environment %q: %w", changes.EnvironmentName, err)
		}
		after, err := marshalEnvironmentVariables(environment, changes.Environment)
		if err != nil {
			return fmt.Errorf("environment %q: %w", changes.EnvironmentName, err)
		}
		targets = append(targets, variableJournalEntry{
			Path: filepath.Join("environments", changes.EnvironmentName+".json"), Before: before,
			After: after, Mode: 0o600,
		})
	}

	journal := variableJournal{Version: 1, Files: targets}
	if err := writeVariableJournal(w.root, journal); err != nil {
		return err
	}
	journaled := true
	defer func() {
		if !journaled {
			return
		}
		if recoveryErr := recoverVariableJournalLocked(w.root); recoveryErr != nil {
			if err == nil {
				err = recoveryErr
			} else {
				err = fmt.Errorf("%v; recovery: %w", err, recoveryErr)
			}
		}
	}()

	for _, target := range targets {
		if err := ctx.Err(); err != nil {
			return err
		}
		path, err := journalTarget(w.root, target.Path)
		if err != nil {
			return err
		}
		if err := writeFileAtomic(path, target.After, os.FileMode(target.Mode)); err != nil {
			return err
		}
	}
	if err := os.Remove(variableJournalPath(w.root)); err != nil {
		return fmt.Errorf("removing variable persistence journal: %w", err)
	}
	journaled = false
	return nil
}

func applyVariableChanges(values *map[string]any, disabled *map[string]bool, changes map[string]VariableChange) error {
	if *values == nil {
		*values = map[string]any{}
	}
	for key, change := range changes {
		if err := model.ValidateVariables(map[string]any{key: change.Value}); err != nil {
			return err
		}
		if change.Unset {
			delete(*values, key)
			if *disabled != nil {
				delete(*disabled, key)
			}
			continue
		}
		(*values)[key] = change.Value
		if *disabled != nil {
			delete(*disabled, key)
		}
	}
	return nil
}

func marshalCollectionVariables(collection model.Collection, changes map[string]VariableChange) ([]byte, error) {
	if err := applyVariableChanges(&collection.Variables, &collection.DisabledVariables, changes); err != nil {
		return nil, err
	}
	if err := collection.Validate(); err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(collection, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func marshalEnvironmentVariables(environment model.Environment, changes map[string]VariableChange) ([]byte, error) {
	if err := applyVariableChanges(&environment.Variables, &environment.DisabledVariables, changes); err != nil {
		return nil, err
	}
	if err := environment.Validate(); err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(environment, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func variableJournalPath(root string) string {
	return filepath.Join(root, DirName, "recovery", variableJournalName)
}

func writeVariableJournal(root string, journal variableJournal) error {
	data, err := json.MarshalIndent(journal, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(variableJournalPath(root), append(data, '\n'), 0o600)
}

func recoverVariableJournal(ctx context.Context, root string) error {
	w := &Workspace{root: root}
	unlock, err := w.lock(ctx)
	if err != nil {
		return recoveryError("acquiring workspace lock: %v", err)
	}
	defer unlock()
	return recoverVariableJournalLocked(root)
}

func recoverVariableJournalLocked(root string) error {
	path := variableJournalPath(root)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return recoveryError("reading %s: %v", path, err)
	}
	var journal variableJournal
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&journal); err != nil {
		return recoveryError("invalid journal: %v", err)
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return recoveryError("invalid journal: trailing data")
	}
	if journal.Version != 1 || len(journal.Files) == 0 || len(journal.Files) > 2 {
		return recoveryError("unsupported journal shape")
	}

	type state struct {
		entry variableJournalEntry
		path  string
		after bool
	}
	states := make([]state, 0, len(journal.Files))
	seen := make(map[string]bool, len(journal.Files))
	allBefore, allAfter := true, true
	for _, entry := range journal.Files {
		target, err := journalTarget(root, entry.Path)
		if err != nil {
			return recoveryError("invalid target %q: %v", entry.Path, err)
		}
		if seen[target] {
			return recoveryError("duplicate target %q", entry.Path)
		}
		seen[target] = true
		current, err := os.ReadFile(target)
		if errors.Is(err, os.ErrNotExist) {
			current = nil
		} else if err != nil {
			return recoveryError("reading target %q: %v", entry.Path, err)
		}
		before, after := bytes.Equal(current, entry.Before), bytes.Equal(current, entry.After)
		if !before && !after {
			return recoveryError("target %q changed outside the journal; restore it or remove the journal after review", entry.Path)
		}
		allBefore = allBefore && before
		allAfter = allAfter && after
		states = append(states, state{entry: entry, path: target, after: after})
	}
	if !allBefore && !allAfter {
		for _, item := range states {
			if item.after {
				if err := writeFileAtomic(item.path, item.entry.Before, os.FileMode(item.entry.Mode)); err != nil {
					return recoveryError("rolling back %q: %v", item.entry.Path, err)
				}
			}
		}
	}
	if err := os.Remove(path); err != nil {
		return recoveryError("removing recovered journal: %v", err)
	}
	return nil
}

func journalTarget(root, relative string) (string, error) {
	if filepath.IsAbs(relative) {
		return "", errors.New("absolute paths are not allowed")
	}
	clean := filepath.Clean(filepath.FromSlash(relative))
	parts := strings.Split(filepath.ToSlash(clean), "/")
	if len(parts) != 2 || (parts[0] != "collections" && parts[0] != "environments") || parts[1] == "" || parts[1] == "." || parts[1] == ".." {
		return "", errors.New("target must be one collection or environment file")
	}
	return filepath.Join(root, DirName, clean), nil
}

func recoveryError(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrRecovery, fmt.Sprintf(format, args...))
}
