package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"req/internal/execution"
)

// runRun implements `req run PATH [flags]`: resolve the saved request, apply
// the flag overrides and execute it. The collection file is never written
// back.
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
	outgoing, err := execution.Prepare(*rp.Item.Request, parsed.ov, parsed.pol)
	if err != nil {
		fmt.Fprintf(stderr, "req: %v\n", err)
		return exitUsage
	}
	return execution.Execute(ctx, outgoing, stdout, stderr)
}

// runArgs is the parsed command line of one `req run` invocation. Overrides
// and policy fields that were not given keep their zero value, which
// execution.Prepare interprets as "keep the saved value".
type runArgs struct {
	path string
	ov   execution.Overrides
	pol  execution.Policy
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
		case "--body":
			v, err := value(&i, "--body")
			if err != nil {
				return runArgs{}, err
			}
			if parsed.ov.BodyMode != "" {
				return runArgs{}, errors.New("--body conflicts with another body flag")
			}
			parsed.ov.BodyMode, parsed.ov.Body = "raw", []byte(v)
		case "--json":
			v, err := value(&i, "--json")
			if err != nil {
				return runArgs{}, err
			}
			if parsed.ov.BodyMode != "" {
				return runArgs{}, errors.New("--json conflicts with another body flag")
			}
			// A {{reference}} may make the text valid JSON only after
			// substitution, so it is passed on verbatim without validation.
			if !strings.Contains(v, "{{") && !json.Valid([]byte(v)) {
				return runArgs{}, errors.New("--json value is not valid JSON")
			}
			parsed.ov.BodyMode, parsed.ov.Body = "json", []byte(v)
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
		case "--insecure":
			parsed.pol.InsecureTLS = true
		case "--fail":
			parsed.pol.FailOnHTTPError = true
		default:
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
	return parsed, nil
}
