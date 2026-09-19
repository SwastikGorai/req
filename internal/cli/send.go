package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/SwastikGorai/req/internal/execution"
	"github.com/SwastikGorai/req/internal/model"
	"github.com/SwastikGorai/req/internal/output"
	"github.com/SwastikGorai/req/internal/store"
)

// sendOptions is the validated configuration of one direct request.
type sendOptions struct {
	variables variableFlags
	method    string
	rawURL    string
	headers   [][2]string // ordered key/value entries; duplicates preserved
	queries   [][2]string // ordered key/value entries; duplicates preserved
	body      *model.Body
	timeout   time.Duration
	insecure  bool
	noFollow  bool
	fail      bool
	output    output.Options
}

// httpToken matches the RFC 9110 token character set, used for methods and
// header names.
var httpToken = regexp.MustCompile(`^[!#$%&'*+\-.^_` + "`" + `|~0-9A-Za-z]+$`)

// runSend executes `req send`. The body goes to stdout; status, elapsed time
// and errors go to stderr. HTTP >=400 is a received response: it exits 4
// only with --fail.
func runSend(ctx context.Context, inv invocation, stdout, stderr io.Writer) int {
	opts, err := parseSendArgs(inv.args)
	if err != nil {
		fmt.Fprintf(stderr, "req: %v\nusage: req send METHOD URL [flags]\n", err)
		return exitUsage
	}
	// A direct send has nothing saved: every value arrives via the
	// overrides and Prepare only validates what parsing already checked.
	var ws *store.Workspace
	if opts.variables.env != "" || inv.workspace != "" {
		var code int
		ws, code = openWorkspace(inv, stderr)
		if ws == nil {
			return code
		}
	}
	if ws == nil && (opts.output.Verbose || opts.output.JSON()) {
		var discoverErr error
		ws, discoverErr = store.Discover("")
		if discoverErr != nil && !errors.Is(discoverErr, store.ErrNoWorkspace) {
			fmt.Fprintf(stderr, "req: %v\n", discoverErr)
			return usageOrStorage(discoverErr)
		}
	}
	if err := prepareOutput(&opts.output, stdout, ws); err != nil {
		fmt.Fprintf(stderr, "req: %v\n", err)
		return usageOrStorage(err)
	}
	scope, err := opts.variables.scope(ctx, ws, nil)
	if err != nil {
		fmt.Fprintf(stderr, "req: %v\n", err)
		return usageOrStorage(err)
	}
	policy := sendPolicy(opts)
	policy.Variables = scope
	outgoing, err := execution.Prepare(model.Request{}, sendOverrides(opts), policy)
	if err != nil {
		fmt.Fprintf(stderr, "req: %v\n", err)
		return exitUsage
	}
	return execution.Execute(ctx, outgoing, stdout, stderr)
}

// sendOverrides translates parsed send flags into execution overrides.
func sendOverrides(opts *sendOptions) execution.Overrides {
	ov := execution.Overrides{
		Auth:    opts.variables.auth,
		Method:  opts.method,
		URL:     opts.rawURL,
		Queries: opts.queries,
		Headers: opts.headers,
	}
	ov.Body = opts.body
	return ov
}

// sendPolicy translates parsed send flags into the execution policy.
func sendPolicy(opts *sendOptions) execution.Policy {
	return execution.Policy{
		Timeout:         opts.timeout,
		InsecureTLS:     opts.insecure,
		FollowRedirects: !opts.noFollow,
		FailOnHTTPError: opts.fail,
		Output:          opts.output,
	}
}

// parseSendArgs parses and validates send arguments. Unknown flags, missing
// values and conflicting modes are errors; nothing is silently dropped.
func parseSendArgs(args []string) (*sendOptions, error) {
	opts := &sendOptions{}
	var positional []string
	var methodFlag, urlFlag string
	haveMethodFlag, haveURLFlag := false, false

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
		case "--header", "-H":
			v, err := value(&i, "--header")
			if err != nil {
				return nil, err
			}
			h, err := parseHeaderEntry(v)
			if err != nil {
				return nil, err
			}
			opts.headers = append(opts.headers, h)
		case "--query":
			v, err := value(&i, "--query")
			if err != nil {
				return nil, err
			}
			q, err := parseQueryEntry(v)
			if err != nil {
				return nil, err
			}
			opts.queries = append(opts.queries, q)
		case "--method":
			if haveMethodFlag {
				return nil, errors.New("--method given more than once")
			}
			v, err := value(&i, "--method")
			if err != nil {
				return nil, err
			}
			haveMethodFlag, methodFlag = true, v
		case "--url":
			if haveURLFlag {
				return nil, errors.New("--url given more than once")
			}
			v, err := value(&i, "--url")
			if err != nil {
				return nil, err
			}
			haveURLFlag, urlFlag = true, v
		case "--body", "--body-file", "--json", "--form", "--form-file", "--urlencoded":
			v, err := value(&i, arg)
			if err != nil {
				return nil, err
			}
			if err := parseBodyFlag(&opts.body, arg, v); err != nil {
				return nil, err
			}
		case "--timeout":
			v, err := value(&i, "--timeout")
			if err != nil {
				return nil, err
			}
			d, perr := time.ParseDuration(v)
			if perr != nil || d <= 0 {
				return nil, fmt.Errorf("invalid --timeout %q (want a positive duration such as 5s)", v)
			}
			opts.timeout = d
		case "--insecure":
			opts.insecure = true
		case "--no-follow":
			opts.noFollow = true
		case "--fail":
			opts.fail = true
		case "--output":
			v, err := value(&i, "--output")
			if err != nil {
				return nil, err
			}
			if v == "" {
				return nil, errors.New("--output requires a non-empty path")
			}
			opts.output.OutputPath = v
		case "--raw":
			opts.output.Raw = true
		case "--verbose":
			opts.output.Verbose = true
		case "--output-format":
			v, err := value(&i, "--output-format")
			if err != nil {
				return nil, err
			}
			opts.output.Format = v
		default:
			if handled, err := opts.variables.parse(args, &i); handled {
				if err != nil {
					return nil, err
				}
				continue
			}
			if strings.HasPrefix(arg, "-") && arg != "-" {
				return nil, fmt.Errorf("unknown flag %q", arg)
			}
			positional = append(positional, arg)
		}
	}

	switch {
	case len(positional) > 2:
		return nil, errors.New("too many arguments (want METHOD and URL)")
	case len(positional) == 2:
		if haveMethodFlag {
			return nil, errors.New("method given both positionally and via --method")
		}
		if haveURLFlag {
			return nil, errors.New("URL given both positionally and via --url")
		}
		opts.method, opts.rawURL = positional[0], positional[1]
	case len(positional) == 1:
		if !haveMethodFlag {
			return nil, errors.New("missing METHOD (positional argument is a URL; use --method)")
		}
		if haveURLFlag {
			return nil, errors.New("URL given both positionally and via --url")
		}
		opts.method, opts.rawURL = methodFlag, positional[0]
	default:
		if !haveMethodFlag || !haveURLFlag {
			return nil, errors.New("missing METHOD and URL")
		}
		opts.method, opts.rawURL = methodFlag, urlFlag
	}

	opts.method = strings.ToUpper(opts.method)
	if !httpToken.MatchString(opts.method) {
		return nil, fmt.Errorf("invalid HTTP method %q", opts.method)
	}
	if u, err := url.Parse(opts.rawURL); !strings.Contains(opts.rawURL, "{{") && (err != nil || (u.Scheme != "http" && u.Scheme != "https")) {
		return nil, fmt.Errorf("URL must be absolute http or https, got %q", opts.rawURL)
	}
	if err := opts.output.Validate(); err != nil {
		return nil, err
	}
	return opts, opts.variables.validate()
}

// parseHeaderEntry validates one "Name: value" header entry.
func parseHeaderEntry(v string) ([2]string, error) {
	name, hvalue, ok := strings.Cut(v, ":")
	name, hvalue = strings.TrimSpace(name), strings.TrimSpace(hvalue)
	if !ok || !httpToken.MatchString(name) {
		return [2]string{}, fmt.Errorf("invalid header %q (want \"Name: value\")", v)
	}
	return [2]string{name, hvalue}, nil
}

// parseQueryEntry validates one "key=value" query entry.
func parseQueryEntry(v string) ([2]string, error) {
	key, qvalue, ok := strings.Cut(v, "=")
	key = strings.TrimSpace(key)
	if !ok || key == "" {
		return [2]string{}, fmt.Errorf("invalid query entry %q (want \"key=value\")", v)
	}
	return [2]string{key, qvalue}, nil
}
