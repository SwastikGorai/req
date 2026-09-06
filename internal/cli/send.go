package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"req/internal/httpclient"
)

type bodyMode uint8

const (
	bodyNone bodyMode = iota
	bodyRaw
	bodyJSON
)

// sendOptions is the validated configuration of one direct request.
type sendOptions struct {
	method   string
	rawURL   string
	headers  http.Header
	queries  [][2]string // ordered key/value entries; duplicates preserved
	bodyMode bodyMode
	body     []byte
	timeout  time.Duration
	insecure bool
	noFollow bool
	fail     bool
}

// httpToken matches the RFC 9110 token character set, used for methods and
// header names.
var httpToken = regexp.MustCompile(`^[!#$%&'*+\-.^_` + "`" + `|~0-9A-Za-z]+$`)

// runSend executes `req send`. The body goes to stdout; status, elapsed time
// and errors go to stderr. HTTP >=400 is a received response: it exits 4
// only with --fail.
func runSend(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	opts, err := parseSendArgs(args)
	if err != nil {
		fmt.Fprintf(stderr, "req: %v\nusage: req send METHOD URL [flags]\n", err)
		return exitUsage
	}
	finalURL, err := applyQueries(opts.rawURL, opts.queries)
	if err != nil {
		fmt.Fprintf(stderr, "req: %v\n", err)
		return exitUsage
	}
	if opts.bodyMode != bodyNone && opts.headers.Get("Content-Type") == "" {
		if opts.bodyMode == bodyJSON {
			opts.headers.Set("Content-Type", "application/json")
		} else {
			opts.headers.Set("Content-Type", "text/plain")
		}
	}

	var body io.Reader
	if opts.bodyMode != bodyNone {
		body = bytes.NewReader(opts.body)
	}
	client := httpclient.Client(httpclient.Options{
		Timeout:         opts.timeout,
		InsecureTLS:     opts.insecure,
		FollowRedirects: !opts.noFollow,
	})
	resp, err := httpclient.Send(ctx, client, opts.method, finalURL, body, opts.headers)
	if err != nil {
		if ctx.Err() != nil {
			fmt.Fprintln(stderr, "req: canceled")
			return exitCanceled
		}
		fmt.Fprintf(stderr, "req: %v\n", err)
		return exitTransport
	}
	defer resp.Body.Close()

	fmt.Fprintf(stderr, "%s %s -> %d %s in %s\n",
		opts.method, finalURL, resp.StatusCode, http.StatusText(resp.StatusCode), resp.Duration.Truncate(time.Microsecond))
	if _, err := io.Copy(stdout, resp.Body); err != nil {
		fmt.Fprintf(stderr, "req: reading body: %v\n", err)
		return exitTransport
	}
	if opts.fail && resp.StatusCode >= http.StatusBadRequest {
		return exitHTTPFail
	}
	return exitSuccess
}

// parseSendArgs parses and validates send arguments. Unknown flags, missing
// values and conflicting modes are errors; nothing is silently dropped.
func parseSendArgs(args []string) (*sendOptions, error) {
	opts := &sendOptions{headers: http.Header{}}
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
			name, hvalue, ok := strings.Cut(v, ":")
			name, hvalue = strings.TrimSpace(name), strings.TrimSpace(hvalue)
			if !ok || !httpToken.MatchString(name) {
				return nil, fmt.Errorf("invalid header %q (want \"Name: value\")", v)
			}
			opts.headers.Add(name, hvalue)
		case "--query":
			v, err := value(&i, "--query")
			if err != nil {
				return nil, err
			}
			key, qvalue, ok := strings.Cut(v, "=")
			key = strings.TrimSpace(key)
			if !ok || key == "" {
				return nil, fmt.Errorf("invalid query entry %q (want \"key=value\")", v)
			}
			opts.queries = append(opts.queries, [2]string{key, qvalue})
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
		case "--body":
			v, err := value(&i, "--body")
			if err != nil {
				return nil, err
			}
			if opts.bodyMode != bodyNone {
				return nil, errors.New("--body conflicts with another body flag")
			}
			opts.bodyMode, opts.body = bodyRaw, []byte(v)
		case "--json":
			v, err := value(&i, "--json")
			if err != nil {
				return nil, err
			}
			if opts.bodyMode != bodyNone {
				return nil, errors.New("--json conflicts with another body flag")
			}
			if !json.Valid([]byte(v)) {
				return nil, errors.New("--json value is not valid JSON")
			}
			opts.bodyMode, opts.body = bodyJSON, []byte(v)
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
		default:
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
	if u, err := url.Parse(opts.rawURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("URL must be absolute http or https, got %q", opts.rawURL)
	}
	return opts, nil
}

// applyQueries appends query entries without re-encoding the components the
// URL already carries. Repeated keys and empty values survive as-is.
func applyQueries(rawURL string, entries [][2]string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	for _, kv := range entries {
		sep := "&"
		if u.RawQuery == "" {
			sep = ""
		}
		u.RawQuery += sep + url.QueryEscape(kv[0]) + "=" + url.QueryEscape(kv[1])
	}
	return u.String(), nil
}
