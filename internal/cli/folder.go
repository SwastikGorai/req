package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"req/internal/model"
)

const folderUsage = `usage: req folder create PATH [--parents]
       req folder rename PATH NEW
       req folder move SRC DEST
       req folder delete PATH [--yes]
`

// stdinIsTerminal reports whether stdin is an interactive terminal;
// injectable for tests.
var stdinIsTerminal = func() bool {
	fi, err := os.Stdin.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// confirmDelete asks the user to confirm a destructive action on the
// terminal; injectable for tests. It returns true only for an explicit
// y/Y/yes answer.
var confirmDelete = func(prompt string, out io.Writer) bool {
	fmt.Fprintf(out, "%s [y/N]: ", prompt)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	}
	return false
}

// requireDeleteApproval gates the destructive delete of a nonempty
// collection or folder. It reports whether the delete may proceed: --yes
// given, an explicit terminal confirmation, or an empty target (nothing can
// be lost, so no question is asked). When it reports no, it prints why not
// and returns the exit code the caller should use: 2 when the input is not
// a terminal and --yes is missing, 0 with a message for a declined prompt —
// deletion aborted, nothing deleted.
func requireDeleteApproval(kind, path string, count int, hasYes bool, stderr io.Writer) (bool, int) {
	if count == 0 || hasYes {
		return true, exitSuccess
	}
	if stdinIsTerminal() {
		noun := "items"
		if count == 1 {
			noun = "item"
		}
		if confirmDelete(fmt.Sprintf("Delete %s %q (%d %s)?", kind, path, count, noun), stderr) {
			return true, exitSuccess
		}
		fmt.Fprintln(stderr, "deletion aborted")
		return false, exitSuccess
	}
	fmt.Fprintf(stderr, "refusing to delete nonempty %s %q (%d items) without confirmation: rerun with --yes\n", kind, path, count)
	return false, exitUsage
}

// runFolder implements `req folder create|rename|move|delete`.
func runFolder(ctx context.Context, inv invocation, stdout, stderr io.Writer) int {
	if len(inv.args) == 0 {
		fmt.Fprintf(stderr, "req: missing folder command (want %q, %q, %q or %q)\n%s", "create", "rename", "move", "delete", folderUsage)
		return exitUsage
	}
	sub, rest := inv.args[0], inv.args[1:]
	subInv := invocation{args: rest, workspace: inv.workspace}
	switch sub {
	case "create":
		return folderCreate(ctx, subInv, stderr)
	case "rename":
		return renameItem(ctx, subInv, "folder rename", stderr)
	case "move":
		return moveItem(ctx, subInv, "folder move", stderr)
	case "delete":
		return folderDelete(ctx, subInv, stderr)
	default:
		fmt.Fprintf(stderr, "req: unknown folder command %q (want %q, %q, %q or %q)\n%s", sub, "create", "rename", "move", "delete", folderUsage)
		return exitUsage
	}
}

// folderCreate implements `req folder create PATH [--parents]`: exactly one
// positional plus the optional --parents flag.
func folderCreate(ctx context.Context, inv invocation, stderr io.Writer) int {
	const createUsage = "usage: req folder create PATH [--parents]\n"

	var (
		positional []string
		parents    bool
	)
	for _, arg := range inv.args {
		switch arg {
		case "--parents":
			parents = true
		default:
			if strings.HasPrefix(arg, "-") && arg != "-" {
				fmt.Fprintf(stderr, "req: unknown flag %q\n%s", arg, createUsage)
				return exitUsage
			}
			positional = append(positional, arg)
		}
	}
	if len(positional) != 1 {
		fmt.Fprintf(stderr, "req: folder create needs exactly one PATH\n%s", createUsage)
		return exitUsage
	}
	path := positional[0]
	ws, code := openWorkspace(inv, stderr)
	if ws == nil {
		return code
	}
	if _, err := ws.CreateFolder(ctx, path, parents); err != nil {
		fmt.Fprintf(stderr, "req: %v\n", err)
		return usageOrStorage(err)
	}
	fmt.Fprintf(stderr, "created folder %q\n", path)
	return exitSuccess
}

// renameItem implements the shared shape of `folder rename PATH NEW` and
// `request rename PATH NEW`: exactly two positionals, no flags. The store
// keeps the item's ID and position; a duplicate sibling name is a usage
// error and a same-name rename is a no-op.
func renameItem(ctx context.Context, inv invocation, command string, stderr io.Writer) int {
	if len(inv.args) != 2 {
		fmt.Fprintf(stderr, "req: %s needs exactly two arguments (PATH NEW)\nusage: req %s PATH NEW\n", command, command)
		return exitUsage
	}
	for _, arg := range inv.args {
		if strings.HasPrefix(arg, "-") && arg != "-" {
			fmt.Fprintf(stderr, "req: unknown flag %q\nusage: req %s PATH NEW\n", arg, command)
			return exitUsage
		}
	}
	path, newName := inv.args[0], inv.args[1]
	if !model.ValidName(newName) {
		fmt.Fprintf(stderr, "req: item name %q is invalid (names must be non-empty, must not be %q or %q, and must contain no / or \\)\n", newName, ".", "..")
		return exitUsage
	}
	ws, code := openWorkspace(inv, stderr)
	if ws == nil {
		return code
	}
	if err := ws.RenameItem(ctx, path, newName); err != nil {
		fmt.Fprintf(stderr, "req: %v\n", err)
		return usageOrStorage(err)
	}
	fmt.Fprintf(stderr, "renamed item %q to %q\n", path, newName)
	return exitSuccess
}

// moveItem implements the shared shape of `folder move SRC DEST` and
// `request move SRC DEST`: exactly two positionals, no flags. DEST is an
// existing folder path or a single-segment collection root; the item keeps
// its name and ID and becomes the last child of the destination.
func moveItem(ctx context.Context, inv invocation, command string, stderr io.Writer) int {
	if len(inv.args) != 2 {
		fmt.Fprintf(stderr, "req: %s needs exactly two arguments (SRC DEST)\nusage: req %s SRC DEST\n", command, command)
		return exitUsage
	}
	for _, arg := range inv.args {
		if strings.HasPrefix(arg, "-") && arg != "-" {
			fmt.Fprintf(stderr, "req: unknown flag %q\nusage: req %s SRC DEST\n", arg, command)
			return exitUsage
		}
	}
	src, dest := inv.args[0], inv.args[1]
	ws, code := openWorkspace(inv, stderr)
	if ws == nil {
		return code
	}
	if err := ws.MoveItem(ctx, src, dest); err != nil {
		fmt.Fprintf(stderr, "req: %v\n", err)
		return usageOrStorage(err)
	}
	fmt.Fprintf(stderr, "moved %q under %q\n", src, dest)
	return exitSuccess
}

// folderDelete implements `req folder delete PATH [--yes]`: deleting a
// nonempty folder requires --yes or an explicit terminal confirmation.
func folderDelete(ctx context.Context, inv invocation, stderr io.Writer) int {
	const deleteUsage = "usage: req folder delete PATH [--yes]\n"

	var (
		positional []string
		yes        bool
	)
	for _, arg := range inv.args {
		switch arg {
		case "--yes":
			yes = true
		default:
			if strings.HasPrefix(arg, "-") && arg != "-" {
				fmt.Fprintf(stderr, "req: unknown flag %q\n%s", arg, deleteUsage)
				return exitUsage
			}
			positional = append(positional, arg)
		}
	}
	if len(positional) != 1 {
		fmt.Fprintf(stderr, "req: folder delete needs exactly one PATH\n%s", deleteUsage)
		return exitUsage
	}
	path := positional[0]
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
		fmt.Fprintf(stderr, "req: %q names the collection (use `req collection delete %s` to delete it)\n", path, path)
		return exitUsage
	case rp.Item.Type != "folder":
		fmt.Fprintf(stderr, "req: %q is a request, not a folder (use `req request delete %s` to delete it)\n", path, path)
		return exitUsage
	}
	if proceed, code := requireDeleteApproval("folder", path, len(rp.Item.Folder.Children), yes, stderr); !proceed {
		return code
	}
	if err := ws.DeleteItem(ctx, path); err != nil {
		fmt.Fprintf(stderr, "req: %v\n", err)
		return usageOrStorage(err)
	}
	if n := len(rp.Item.Folder.Children); n > 0 {
		fmt.Fprintf(stderr, "deleted folder %q (%d child items)\n", path, n)
	} else {
		fmt.Fprintf(stderr, "deleted folder %q\n", path)
	}
	return exitSuccess
}
