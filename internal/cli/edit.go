package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"req/internal/model"
	"req/internal/store"
)

// runEditor launches the editor argv without a shell and waits; injectable
// for tests (the default wires the process's stdio).
var runEditor = func(argv []string) error {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

// editRequest implements `req request edit PATH`: open the saved request's
// JSON in $EDITOR/$VISUAL, validate the edited result and replace it under
// the source revision. Invalid edits are preserved in a recovery file.
func editRequest(ctx context.Context, inv invocation, stderr io.Writer) int {
	const editUsage = "usage: req request edit PATH\n"

	if len(inv.args) != 1 {
		fmt.Fprintf(stderr, "req: request edit needs exactly one PATH\n%s", editUsage)
		return exitUsage
	}
	if strings.HasPrefix(inv.args[0], "-") && inv.args[0] != "-" {
		fmt.Fprintf(stderr, "req: unknown flag %q\n%s", inv.args[0], editUsage)
		return exitUsage
	}
	path := inv.args[0]
	ws, code := openWorkspace(inv, stderr)
	if ws == nil {
		return code
	}
	rp, err := ws.ResolvePath(ctx, path)
	if err != nil {
		fmt.Fprintf(stderr, "req: %v\n", err)
		return usageOrStorage(err)
	}
	switch {
	case rp.Item == nil:
		fmt.Fprintf(stderr, "req: %q names the collection, not a request\n", path)
		return exitUsage
	case rp.Item.Type != "request":
		fmt.Fprintf(stderr, "req: %q is a folder, not a request\n", path)
		return exitUsage
	}

	// Seed a temp file with the saved request for the editor to open.
	original, err := json.MarshalIndent(rp.Item.Request, "", "  ")
	if err != nil {
		fmt.Fprintf(stderr, "req: %v\n", err)
		return exitStorage
	}
	original = append(original, '\n')
	tmp, err := os.CreateTemp("", "req-edit-*.json")
	if err != nil {
		fmt.Fprintf(stderr, "req: %v\n", err)
		return exitStorage
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(original); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		fmt.Fprintf(stderr, "req: %v\n", err)
		return exitStorage
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		fmt.Fprintf(stderr, "req: %v\n", err)
		return exitStorage
	}

	// EDITOR wins over VISUAL; a value that is blank after trimming counts
	// as unset.
	editor := ""
	for _, value := range []string{os.Getenv("EDITOR"), os.Getenv("VISUAL")} {
		if strings.TrimSpace(value) != "" {
			editor = value
			break
		}
	}
	if editor == "" {
		os.Remove(tmpPath)
		fmt.Fprintf(stderr, "req: no editor configured: set EDITOR or VISUAL to an editor command (for example EDITOR=notepad or EDITOR='code --wait')\n")
		return exitUsage
	}

	// Split the editor value into argv and run it without a shell, so one
	// argv element is exactly one argument.
	argv := append(strings.Fields(editor), tmpPath)
	if err := runEditor(argv); err != nil {
		// Keep the temp file: the user's edit survives there.
		fmt.Fprintf(stderr, "req: editor failed: %v (your edit is preserved at %s)\n", err, tmpPath)
		return exitUsage
	}

	edited, err := os.ReadFile(tmpPath)
	if err != nil {
		os.Remove(tmpPath)
		fmt.Fprintf(stderr, "req: %v\n", err)
		return exitStorage
	}
	if bytes.Equal(original, edited) {
		os.Remove(tmpPath)
		fmt.Fprintf(stderr, "request %q unchanged\n", path)
		return exitSuccess
	}

	// Strict decode: unknown fields and trailing data make the edit invalid.
	var editedReq model.Request
	dec := json.NewDecoder(bytes.NewReader(edited))
	dec.DisallowUnknownFields()
	decErr := dec.Decode(&editedReq)
	if decErr == nil && dec.More() {
		decErr = errors.New("trailing data after the JSON document")
	}
	if decErr != nil {
		if recovery, rerr := ws.SaveRecovery(rp.Collection.ID, edited); rerr == nil && recovery != "" {
			os.Remove(tmpPath)
			fmt.Fprintf(stderr, "req: edited request is invalid: %v — your edit is preserved at %s\n", decErr, recovery)
		} else {
			fmt.Fprintf(stderr, "req: edited request is invalid: %v — your edit is preserved at %s\n", decErr, tmpPath)
		}
		return exitUsage
	}

	// UpdateRequest persists only if the collection is still at the
	// revision this edit started from, checking under the workspace lock:
	// a competing save keeps the disk and the stale edit is preserved in
	// recovery.
	if err := ws.UpdateRequest(ctx, path, editedReq, rp.Rev); err != nil {
		var conflict *store.ConflictError
		if errors.As(err, &conflict) {
			// The rejected candidate is already in recovery.
			os.Remove(tmpPath)
			if conflict.Recovery != "" {
				fmt.Fprintf(stderr, "req: %v (your edit is preserved at %s)\n", err, conflict.Recovery)
			} else {
				fmt.Fprintf(stderr, "req: %v\n", err)
			}
			return exitStorage
		}
		// Nothing preserved the edit yet, so keep the temp file.
		fmt.Fprintf(stderr, "req: %v (your edit is preserved at %s)\n", err, tmpPath)
		return usageOrStorage(err)
	}
	os.Remove(tmpPath)
	fmt.Fprintf(stderr, "updated request %q\n", path)
	return exitSuccess
}
