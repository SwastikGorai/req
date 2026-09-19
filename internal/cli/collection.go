package cli

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/SwastikGorai/req/internal/model"
)

const collectionUsage = `usage: req collection create NAME
       req collection list
       req collection rename OLD NEW
       req collection delete NAME [--yes]
`

// runCollection implements `req collection create|list|rename|delete`.
func runCollection(ctx context.Context, inv invocation, stdout, stderr io.Writer) int {
	if len(inv.args) == 0 {
		fmt.Fprintf(stderr, "req: missing collection command (want %q, %q, %q or %q)\n%s", "create", "list", "rename", "delete", collectionUsage)
		return exitUsage
	}
	sub, rest := inv.args[0], inv.args[1:]
	subInv := invocation{args: rest, workspace: inv.workspace}
	switch sub {
	case "create":
		return collectionCreate(ctx, subInv, stderr)
	case "list":
		return collectionList(ctx, subInv, stdout, stderr)
	case "rename":
		return collectionRename(ctx, subInv, stderr)
	case "delete":
		return collectionDelete(ctx, subInv, stderr)
	default:
		fmt.Fprintf(stderr, "req: unknown collection command %q (want %q, %q, %q or %q)\n%s", sub, "create", "list", "rename", "delete", collectionUsage)
		return exitUsage
	}
}

// collectionCreate implements `req collection create NAME`: exactly one
// positional, no flags.
func collectionCreate(ctx context.Context, inv invocation, stderr io.Writer) int {
	if len(inv.args) != 1 {
		fmt.Fprintf(stderr, "req: collection create needs exactly one NAME\n%s", collectionUsage)
		return exitUsage
	}
	for _, arg := range inv.args {
		if strings.HasPrefix(arg, "-") && arg != "-" {
			fmt.Fprintf(stderr, "req: unknown flag %q\n%s", arg, collectionUsage)
			return exitUsage
		}
	}
	name := inv.args[0]
	if !model.ValidName(name) {
		fmt.Fprintf(stderr, "req: collection name %q is invalid (names must be non-empty, must not be %q or %q, and must contain no / or \\)\n", name, ".", "..")
		return exitUsage
	}
	ws, code := openWorkspace(inv, stderr)
	if ws == nil {
		return code
	}
	c, err := ws.CreateCollection(ctx, name)
	if err != nil {
		fmt.Fprintf(stderr, "req: %v\n", err)
		return usageOrStorage(err)
	}
	fmt.Fprintf(stderr, "created collection %q (%s)\n", name, c.ID)
	return exitSuccess
}

// collectionList implements `req collection list`: NAME<TAB>ID per line,
// sorted by name, on stdout.
func collectionList(ctx context.Context, inv invocation, stdout, stderr io.Writer) int {
	if len(inv.args) != 0 {
		fmt.Fprintf(stderr, "req: unknown argument %q for collection list\n%s", inv.args[0], collectionUsage)
		return exitUsage
	}
	ws, code := openWorkspace(inv, stderr)
	if ws == nil {
		return code
	}
	colls, err := ws.ListCollections(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "req: %v\n", err)
		return usageOrStorage(err)
	}
	for _, c := range colls {
		fmt.Fprintf(stdout, "%s\t%s\n", c.Name, c.ID)
	}
	return exitSuccess
}

// collectionRename implements `req collection rename OLD NEW`: exactly two
// positionals, no flags. The file name and ID stay; only the stored name
// changes. A same-name rename is a no-op.
func collectionRename(ctx context.Context, inv invocation, stderr io.Writer) int {
	if len(inv.args) != 2 {
		fmt.Fprintf(stderr, "req: collection rename needs exactly two arguments (OLD NEW)\n%s", collectionUsage)
		return exitUsage
	}
	for _, arg := range inv.args {
		if strings.HasPrefix(arg, "-") && arg != "-" {
			fmt.Fprintf(stderr, "req: unknown flag %q\n%s", arg, collectionUsage)
			return exitUsage
		}
	}
	oldName, newName := inv.args[0], inv.args[1]
	if !model.ValidName(newName) {
		fmt.Fprintf(stderr, "req: collection name %q is invalid (names must be non-empty, must not be %q or %q, and must contain no / or \\)\n", newName, ".", "..")
		return exitUsage
	}
	ws, code := openWorkspace(inv, stderr)
	if ws == nil {
		return code
	}
	if err := ws.RenameCollection(ctx, oldName, newName); err != nil {
		fmt.Fprintf(stderr, "req: %v\n", err)
		return usageOrStorage(err)
	}
	fmt.Fprintf(stderr, "renamed collection %q to %q\n", oldName, newName)
	return exitSuccess
}

// collectionDelete implements `req collection delete NAME [--yes]`: an
// empty collection deletes outright, a nonempty one needs --yes or an
// explicit terminal confirmation.
func collectionDelete(ctx context.Context, inv invocation, stderr io.Writer) int {
	const deleteUsage = "usage: req collection delete NAME [--yes]\n"

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
		fmt.Fprintf(stderr, "req: collection delete needs exactly one NAME\n%s", deleteUsage)
		return exitUsage
	}
	name := positional[0]
	ws, code := openWorkspace(inv, stderr)
	if ws == nil {
		return code
	}
	rp, err := ws.ResolvePath(ctx, name)
	if err != nil {
		fmt.Fprintf(stderr, "req: %v\n", err)
		return usageOrStorage(err)
	}
	if rp.Item != nil {
		fmt.Fprintf(stderr, "req: %q names an item, not a collection (items are deleted with folder delete or request delete)\n", name)
		return exitUsage
	}
	if proceed, code := requireDeleteApproval("collection", name, len(rp.Collection.Items), yes, stderr); !proceed {
		return code
	}
	if err := ws.DeleteCollection(ctx, name, rp.Rev); err != nil {
		fmt.Fprintf(stderr, "req: %v\n", err)
		return usageOrStorage(err)
	}
	fmt.Fprintf(stderr, "deleted collection %q\n", name)
	return exitSuccess
}
