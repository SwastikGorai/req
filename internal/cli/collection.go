package cli

import (
	"context"
	"fmt"
	"io"
	"strings"

	"req/internal/model"
)

const collectionUsage = "usage: req collection create NAME\n       req collection list\n"

// runCollection implements `req collection create|list`.
func runCollection(ctx context.Context, inv invocation, stdout, stderr io.Writer) int {
	if len(inv.args) == 0 {
		fmt.Fprintf(stderr, "req: missing collection command (want %q or %q)\n%s", "create", "list", collectionUsage)
		return exitUsage
	}
	sub, rest := inv.args[0], inv.args[1:]
	subInv := invocation{args: rest, workspace: inv.workspace}
	switch sub {
	case "create":
		return collectionCreate(ctx, subInv, stderr)
	case "list":
		return collectionList(ctx, subInv, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "req: unknown collection command %q (want %q or %q)\n%s", sub, "create", "list", collectionUsage)
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
