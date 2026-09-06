package cli

import (
	"context"
	"fmt"
	"io"
	"strings"
)

const folderUsage = "usage: req folder create PATH [--parents]\n"

// runFolder implements `req folder create PATH [--parents]`.
func runFolder(ctx context.Context, inv invocation, stdout, stderr io.Writer) int {
	if len(inv.args) == 0 {
		fmt.Fprintf(stderr, "req: missing folder command (want %q)\n%s", "create", folderUsage)
		return exitUsage
	}
	sub, rest := inv.args[0], inv.args[1:]
	if sub != "create" {
		fmt.Fprintf(stderr, "req: unknown folder command %q (want %q)\n%s", sub, "create", folderUsage)
		return exitUsage
	}
	var (
		positional []string
		parents    bool
	)
	for _, arg := range rest {
		switch arg {
		case "--parents":
			parents = true
		default:
			if strings.HasPrefix(arg, "-") && arg != "-" {
				fmt.Fprintf(stderr, "req: unknown flag %q\n%s", arg, folderUsage)
				return exitUsage
			}
			positional = append(positional, arg)
		}
	}
	if len(positional) != 1 {
		fmt.Fprintf(stderr, "req: folder create needs exactly one PATH\n%s", folderUsage)
		return exitUsage
	}
	path := positional[0]
	subInv := invocation{args: rest, workspace: inv.workspace}
	ws, code := openWorkspace(subInv, stderr)
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
