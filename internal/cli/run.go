package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"req/internal/execution"
	"req/internal/store"
	"req/internal/variables"
)

// runRun implements `req run PATH [flags]`: resolve the saved request, apply
// the flag overrides and execute it. Variable writes are opt-in.
func runRun(ctx context.Context, inv invocation, stdout, stderr io.Writer) int {
	parsed, err := parseRunArgs(inv.args)
	if err != nil {
		fmt.Fprintf(stderr, "req: %v\nusage: req run PATH [flags]\n", err)
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
	switch {
	case rp.Item == nil:
		fmt.Fprintf(stderr, "req: %q names the collection, not a request\n", parsed.path)
		return exitUsage
	case rp.Item.Type != "request":
		fmt.Fprintf(stderr, "req: %q is a folder, not a request\n", parsed.path)
		return exitUsage
	}
	if rp.Item.Request.Import.Blocked() {
		fmt.Fprintf(stderr, "req: imported request %q is blocked: %s\n", parsed.path, strings.Join(rp.Item.Request.Import.Unsupported, "; "))
		return exitUsage
	}
	scope, err := parsed.variables.scope(ctx, ws, rp.Collection.ActiveVariables())
	if err != nil {
		fmt.Fprintf(stderr, "req: %v\n", err)
		return usageOrStorage(err)
	}
	parsed.pol.Variables = scope
	parsed.pol.BodyBase = ws.Root()
	if parsed.persistVars {
		convert := func(src map[string]variables.Change) map[string]store.VariableChange {
			if len(src) == 0 {
				return nil
			}
			dst := make(map[string]store.VariableChange, len(src))
			for key, change := range src {
				dst[key] = store.VariableChange{Value: change.Value, Unset: change.Unset}
			}
			return dst
		}
		parsed.sp.Persist = func(ctx context.Context) error {
			return ws.PersistVariables(ctx, store.VariableChanges{
				CollectionName:      rp.Collection.Name,
				CollectionRevision:  rp.Rev,
				Collection:          convert(scope.CollectionChanges()),
				EnvironmentName:     parsed.variables.env,
				EnvironmentRevision: parsed.variables.envRevision,
				Environment:         convert(scope.EnvironmentChanges()),
			})
		}
	}
	if rp.Item.Request.FollowRedirects != nil && parsed.pol.FollowRedirects {
		parsed.pol.FollowRedirects = *rp.Item.Request.FollowRedirects
	}
	if rp.Item.Request.InsecureTLS {
		parsed.pol.InsecureTLS = true
	}
	saved := *rp.Item.Request
	saved.Auth = rp.Auth
	pre, post, err := execution.InheritedScripts(rp.Collection, rp.Segments)
	if err != nil {
		fmt.Fprintf(stderr, "req: %v\n", err)
		return exitUsage
	}
	return execution.RunLifecycle(ctx, saved, parsed.ov, parsed.pol, parsed.sp, pre, post, stdout, stderr)
}

// runArgs is the parsed command line of one `req run` invocation. Overrides
// and policy fields that were not given keep their zero value, which
// execution.Prepare interprets as "keep the saved value".
type runArgs struct {
	variables   variableFlags
	path        string
	ov          execution.Overrides
	pol         execution.Policy
	sp          execution.ScriptPolicy
	persistVars bool
}

// parseRunArgs parses and validates run arguments. Unknown flags, missing
// values and conflicting modes are errors; nothing is silently dropped.
func parseRunArgs(args []string) (runArgs, error) {
	var (
		parsed              runArgs
		positional          []string
		haveMethod, haveURL bool
		noFollow            bool
	)
	value := func(i *int, name string) (string, error) {
		*i++
		if *i >= len(args) {
			return "", fmt.Errorf("flag %s requires a value", name)
		}
		return args[*i], nil
	}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--method":
			if haveMethod {
				return runArgs{}, errors.New("--method given more than once")
			}
			v, err := value(&i, "--method")
			if err != nil {
				return runArgs{}, err
			}
			haveMethod, parsed.ov.Method = true, v
		case "--url":
			if haveURL {
				return runArgs{}, errors.New("--url given more than once")
			}
			v, err := value(&i, "--url")
			if err != nil {
				return runArgs{}, err
			}
			haveURL, parsed.ov.URL = true, v
		case "--header", "-H":
			v, err := value(&i, "--header")
			if err != nil {
				return runArgs{}, err
			}
			h, err := parseHeaderEntry(v)
			if err != nil {
				return runArgs{}, err
			}
			parsed.ov.Headers = append(parsed.ov.Headers, h)
		case "--query":
			v, err := value(&i, "--query")
			if err != nil {
				return runArgs{}, err
			}
			q, err := parseQueryEntry(v)
			if err != nil {
				return runArgs{}, err
			}
			parsed.ov.Queries = append(parsed.ov.Queries, q)
		case "--body", "--body-file", "--json", "--form", "--form-file", "--urlencoded":
			v, err := value(&i, arg)
			if err != nil {
				return runArgs{}, err
			}
			if err := parseBodyFlag(&parsed.ov.Body, arg, v); err != nil {
				return runArgs{}, err
			}
		case "--timeout":
			v, err := value(&i, "--timeout")
			if err != nil {
				return runArgs{}, err
			}
			d, perr := time.ParseDuration(v)
			if perr != nil || d <= 0 {
				return runArgs{}, fmt.Errorf("invalid --timeout %q (want a positive duration such as 5s)", v)
			}
			parsed.pol.Timeout = d
		case "--no-follow":
			noFollow = true
		case "--no-scripts":
			parsed.sp.Disabled = true
		case "--persist-vars":
			parsed.persistVars = true
		case "--script-timeout":
			v, err := value(&i, "--script-timeout")
			if err != nil {
				return runArgs{}, err
			}
			d, perr := time.ParseDuration(v)
			if perr != nil || d <= 0 {
				return runArgs{}, fmt.Errorf("invalid --script-timeout %q (want a positive duration such as 5s)", v)
			}
			parsed.sp.Timeout = d
		case "--insecure":
			parsed.pol.InsecureTLS = true
		case "--fail":
			parsed.pol.FailOnHTTPError = true
		default:
			if handled, err := parsed.variables.parse(args, &i); handled {
				if err != nil {
					return runArgs{}, err
				}
				continue
			}
			if strings.HasPrefix(arg, "-") && arg != "-" {
				return runArgs{}, fmt.Errorf("unknown flag %q", arg)
			}
			positional = append(positional, arg)
		}
	}
	if len(positional) != 1 {
		return runArgs{}, errors.New("run needs exactly one PATH")
	}
	parsed.path = positional[0]
	parsed.pol.FollowRedirects = !noFollow
	parsed.ov.Auth = parsed.variables.auth
	return parsed, parsed.variables.validate()
}
