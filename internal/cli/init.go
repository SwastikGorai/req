package cli

import (
	"fmt"
	"io"

	"req/internal/store"
)

// invocation is one parsed command line: the subcommand with its own
// arguments, plus the global flags. Global flags are only recognized before
// the subcommand so they can never collide with a command flag's value.
type invocation struct {
	args      []string
	workspace string // explicit --workspace path; "" means discover
}

// parseGlobals extracts leading global flags (--workspace PATH,
// --workspace=PATH, --no-color). It stops at the first non-flag argument.
func parseGlobals(args []string) (invocation, error) {
	inv := invocation{args: args}
	for len(inv.args) > 0 {
		arg := inv.args[0]
		switch {
		case arg == "--workspace":
			if len(inv.args) < 2 {
				return inv, fmt.Errorf("--workspace requires a path")
			}
			inv.workspace = inv.args[1]
			inv.args = inv.args[2:]
		case len(arg) > len("--workspace=") && arg[:len("--workspace=")] == "--workspace=":
			inv.workspace = arg[len("--workspace="):]
			inv.args = inv.args[1:]
		case arg == "--no-color":
			inv.args = inv.args[1:] // accepted; output formatting arrives in Phase 18
		default:
			return inv, nil
		}
	}
	return inv, nil
}

// runInit implements `req init [--workspace PATH]`: create the workspace in
// the explicit or current directory, idempotently. --workspace is accepted
// both before and after the subcommand (init has no other value-taking
// flags, so the position is unambiguous).
func runInit(inv invocation, stdout, stderr io.Writer) int {
	dir := inv.workspace
	for i := 0; i < len(inv.args); i++ {
		arg := inv.args[i]
		var path string
		switch {
		case arg == "--workspace":
			if i+1 >= len(inv.args) {
				fmt.Fprintf(stderr, "req: --workspace requires a path\n")
				return exitUsage
			}
			i++
			path = inv.args[i]
		case len(arg) > len("--workspace=") && arg[:len("--workspace=")] == "--workspace=":
			path = arg[len("--workspace="):]
		default:
			fmt.Fprintf(stderr, "req: unknown argument %q for init\n", arg)
			return exitUsage
		}
		if dir != "" {
			fmt.Fprintf(stderr, "req: --workspace given more than once\n")
			return exitUsage
		}
		dir = path
	}
	if dir == "" {
		var err error
		if dir, err = osGetwd(); err != nil {
			fmt.Fprintf(stderr, "req: %v\n", err)
			return exitStorage
		}
	}
	ws, created, err := store.Init(dir)
	if err != nil {
		fmt.Fprintf(stderr, "req: %v\n", err)
		return exitStorage
	}
	if created {
		fmt.Fprintf(stdout, "initialized req workspace at %s\n", ws.Root())
	} else {
		fmt.Fprintf(stdout, "workspace already initialized at %s\n", ws.Root())
	}
	return exitSuccess
}
