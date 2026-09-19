// Package store owns workspace discovery, the on-disk layout, mutation
// locking and atomic persistence of the native JSON files.
package store

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// DirName is the workspace marker directory created by Init and found by
// Discover.
const DirName = ".req"

// Workspace is a directory tree containing a .req directory.
type Workspace struct {
	root string // absolute path of the directory containing .req
}

// Root returns the absolute workspace root (the parent of .req).
func (w *Workspace) Root() string { return w.root }

// Dir returns the absolute path of the .req directory itself.
func (w *Workspace) Dir() string { return filepath.Join(w.root, DirName) }

// gitignoreContent keeps generated and secret-bearing state out of version
// control; collections themselves may be committed.
const gitignoreContent = `# req workspace — collections may be version-controlled
environments/
recovery/
*.lock
*.tmp*
`

// Init creates a workspace under dir and returns it. It is idempotent:
// existing configuration (config.json, .gitignore) is never overwritten;
// missing directories are created. The bool reports whether the workspace
// was newly created.
func Init(dir string) (*Workspace, bool, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, false, err
	}
	reqDir := filepath.Join(abs, DirName)
	created := !dirExists(reqDir)
	for _, sub := range []string{reqDir, filepath.Join(reqDir, "collections"), filepath.Join(reqDir, "environments"), filepath.Join(reqDir, "recovery")} {
		if err := os.MkdirAll(sub, 0o755); err != nil {
			return nil, false, fmt.Errorf("creating %s: %w", sub, err)
		}
	}
	if err := recoverVariableJournal(context.Background(), abs); err != nil {
		return nil, false, err
	}
	configPath := filepath.Join(reqDir, "config.json")
	if !fileExists(configPath) {
		data, _ := json.MarshalIndent(map[string]interface{}{"schema_version": 1}, "", "  ")
		if err := writeFileAtomic(configPath, append(data, '\n'), 0o644); err != nil {
			return nil, false, err
		}
	}
	gitignorePath := filepath.Join(reqDir, ".gitignore")
	if !fileExists(gitignorePath) {
		if err := writeFileAtomic(gitignorePath, []byte(gitignoreContent), 0o644); err != nil {
			return nil, false, err
		}
	}
	return &Workspace{root: abs}, created, nil
}

// Discover returns the closest workspace covering start ("" means the
// process working directory): the nearest ancestor containing .req.
func Discover(start string) (*Workspace, error) {
	dir := start
	if dir == "" {
		var err error
		if dir, err = os.Getwd(); err != nil {
			return nil, err
		}
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	for {
		if dirExists(filepath.Join(abs, DirName)) {
			if err := recoverVariableJournal(context.Background(), abs); err != nil {
				return nil, err
			}
			return &Workspace{root: abs}, nil
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			return nil, fmt.Errorf("%w: no %s directory in %s or its ancestors", ErrNoWorkspace, DirName, dir)
		}
		abs = parent
	}
}

// Open resolves an explicit --workspace path: either a directory containing
// .req or a .req directory itself.
func Open(path string) (*Workspace, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if filepath.Base(abs) == DirName && dirExists(abs) {
		if err := recoverVariableJournal(context.Background(), filepath.Dir(abs)); err != nil {
			return nil, err
		}
		return &Workspace{root: filepath.Dir(abs)}, nil
	}
	if dirExists(filepath.Join(abs, DirName)) {
		if err := recoverVariableJournal(context.Background(), abs); err != nil {
			return nil, err
		}
		return &Workspace{root: abs}, nil
	}
	return nil, fmt.Errorf("%w: %s is not a workspace", ErrNoWorkspace, path)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
