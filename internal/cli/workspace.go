package cli

import (
	"errors"
	"fmt"
	"io"

	"req/internal/store"
)

// openWorkspace resolves the workspace from an explicit --workspace path or,
// when none was given, by discovering the closest ancestor of the working
// directory. On failure it prints a diagnostic to stderr and returns the
// process exit code (2).
func openWorkspace(inv invocation, stderr io.Writer) (*store.Workspace, int) {
	var (
		ws  *store.Workspace
		err error
	)
	if inv.workspace != "" {
		ws, err = store.Open(inv.workspace)
	} else {
		ws, err = store.Discover("")
	}
	if err != nil {
		fmt.Fprintf(stderr, "req: %v\nhint: run `req init` to create a workspace here, or pass --workspace PATH\n", err)
		return nil, exitUsage
	}
	return ws, exitSuccess
}

// usageOrStorage maps store errors to exit codes: not-found paths, invalid
// paths and duplicate names are usage problems (2); everything else (IO,
// lock, conflict, malformed file) is a storage failure (7).
func usageOrStorage(err error) int {
	if errors.Is(err, store.ErrNotFound) ||
		errors.Is(err, store.ErrInvalidPath) ||
		errors.Is(err, store.ErrDuplicateName) {
		return exitUsage
	}
	return exitStorage
}
