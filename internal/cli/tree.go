package cli

import (
	"context"
	"fmt"
	"io"
	"strings"

	"req/internal/model"
)

// runTree implements `req tree PATH`: the collection (or resolved folder) as
// the root line, then the subtree in stored order on stdout.
func runTree(ctx context.Context, inv invocation, stdout, stderr io.Writer) int {
	if len(inv.args) != 1 {
		fmt.Fprintf(stderr, "req: tree needs exactly one PATH\nusage: req tree PATH\n")
		return exitUsage
	}
	ws, code := openWorkspace(inv, stderr)
	if ws == nil {
		return code
	}
	rp, err := ws.ResolvePath(ctx, inv.args[0])
	if err != nil {
		fmt.Fprintf(stderr, "req: %v\n", err)
		return usageOrStorage(err)
	}
	var name string
	var children []model.Item
	switch {
	case rp.Item == nil:
		name = rp.Collection.Name
		children = rp.Collection.Items
	case rp.Item.Type == "folder":
		name = rp.Item.Name
		children = rp.Item.Folder.Children
	default:
		fmt.Fprintf(stderr, "req: %q is a request, not a folder or collection\n", inv.args[0])
		return exitUsage
	}
	fmt.Fprintln(stdout, name)
	writeTree(stdout, children, 0)
	return exitSuccess
}

// writeTree renders items at the given depth — two spaces per level below
// the first — in stored order: folders as "Name/" with their children
// beneath, requests as "Name  METHOD URL".
func writeTree(w io.Writer, items []model.Item, depth int) {
	indent := strings.Repeat("  ", depth)
	for _, it := range items {
		switch it.Type {
		case "folder":
			fmt.Fprintf(w, "%s%s/\n", indent, it.Name)
			writeTree(w, it.Folder.Children, depth+1)
		case "request":
			fmt.Fprintf(w, "%s%s  %s %s\n", indent, it.Name, it.Request.Method, it.Request.URL)
		}
	}
}
