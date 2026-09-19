package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/SwastikGorai/req/internal/execution"
	"github.com/SwastikGorai/req/internal/exporter"
	"github.com/SwastikGorai/req/internal/model"
	"github.com/SwastikGorai/req/internal/variables"
)

const exportUsage = `usage: req export curl PATH [--resolve --env NAME] [--strict]
       --resolve substitutes saved values and may expose secrets
`

type exportArgs struct {
	path    string
	env     string
	resolve bool
	strict  bool
}

func runExport(ctx context.Context, inv invocation, stdout, stderr io.Writer) int {
	parsed, err := parseExportArgs(inv.args)
	if err != nil {
		fmt.Fprintf(stderr, "req: %v\n%s", err, exportUsage)
		return exitUsage
	}
	ws, code := openWorkspace(inv, stderr)
	if ws == nil {
		return code
	}
	rp, err := ws.ResolvePath(ctx, parsed.path)
	if err != nil {
		fmt.Fprintf(stderr, "req: %v\n", err)
		return usageOrStorage(err)
	}
	if rp.Item == nil {
		fmt.Fprintf(stderr, "req: %q names the collection, not a request\n", parsed.path)
		return exitUsage
	}
	if rp.Item.Type != "request" {
		fmt.Fprintf(stderr, "req: %q is a folder, not a request\n", parsed.path)
		return exitUsage
	}

	saved := *rp.Item.Request
	saved.Auth = rp.Auth
	pre, post, err := execution.InheritedScripts(rp.Collection, rp.Segments)
	if err != nil {
		fmt.Fprintf(stderr, "req: %v\n", err)
		return exitUsage
	}
	if len(pre) > 0 || len(post) > 0 {
		saved.Scripts = &model.Scripts{PreRequest: pre, PostResponse: post}
	}

	var scope *variables.Scope
	if parsed.resolve {
		flags := variableFlags{env: parsed.env}
		scope, err = flags.scope(ctx, ws, rp.Collection.ActiveVariables())
		if err != nil {
			fmt.Fprintf(stderr, "req: %v\n", err)
			return usageOrStorage(err)
		}
	}
	result, err := exporter.ExportCurl(saved, exporter.Options{
		Resolve:   parsed.resolve,
		Variables: scope,
		Strict:    parsed.strict,
	})
	if err != nil {
		printExportError(stderr, err)
		return exitUsage
	}
	printExportWarnings(stderr, result.Warnings)
	fmt.Fprintln(stdout, result.Command)
	return exitSuccess
}

func parseExportArgs(args []string) (exportArgs, error) {
	if len(args) == 0 || args[0] != "curl" {
		return exportArgs{}, errors.New("export needs the curl format")
	}
	var parsed exportArgs
	var positional []string
	for i := 1; i < len(args); i++ {
		switch arg := args[i]; arg {
		case "--strict":
			if parsed.strict {
				return exportArgs{}, errors.New("--strict given more than once")
			}
			parsed.strict = true
		case "--resolve":
			if parsed.resolve {
				return exportArgs{}, errors.New("--resolve given more than once")
			}
			parsed.resolve = true
		case "--env":
			if i+1 >= len(args) {
				return exportArgs{}, errors.New("--env requires a value")
			}
			i++
			if parsed.env != "" {
				return exportArgs{}, errors.New("--env given more than once")
			}
			parsed.env = args[i]
		case "--":
			positional = append(positional, args[i+1:]...)
			i = len(args)
		default:
			if strings.HasPrefix(arg, "--env=") {
				if parsed.env != "" {
					return exportArgs{}, errors.New("--env given more than once")
				}
				parsed.env = strings.TrimPrefix(arg, "--env=")
				continue
			}
			if strings.HasPrefix(arg, "-") && arg != "-" {
				return exportArgs{}, fmt.Errorf("unknown flag %q", arg)
			}
			positional = append(positional, arg)
		}
	}
	if len(positional) != 1 {
		return exportArgs{}, errors.New("export curl needs exactly one PATH")
	}
	parsed.path = positional[0]
	if parsed.env != "" && !model.ValidEnvironmentName(parsed.env) {
		return exportArgs{}, fmt.Errorf("invalid environment name %q", parsed.env)
	}
	if parsed.resolve && parsed.env == "" {
		return exportArgs{}, errors.New("--resolve requires --env NAME (resolved output may expose secrets)")
	}
	if !parsed.resolve && parsed.env != "" {
		return exportArgs{}, errors.New("--env is only valid with --resolve")
	}
	return parsed, nil
}

func printExportWarnings(w io.Writer, warnings []exporter.Warning) {
	for _, warning := range exporter.SortedWarnings(warnings) {
		if warning.Path == "" {
			fmt.Fprintf(w, "warning [%s]: %s\n", warning.Code, warning.Message)
		} else {
			fmt.Fprintf(w, "warning [%s] %s: %s\n", warning.Code, warning.Path, warning.Message)
		}
	}
}

func printExportError(w io.Writer, err error) {
	var strict *exporter.StrictError
	if errors.As(err, &strict) {
		printExportWarnings(w, strict.Warnings)
	}
	fmt.Fprintf(w, "req: %v\n", err)
}
