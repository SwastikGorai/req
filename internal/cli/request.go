package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"req/internal/model"
	"req/internal/store"
)

const requestUsage = "usage: req request create PATH --method METHOD --url URL [flags]\n       req request list PATH\n       req request show PATH\n"

// runRequest implements `req request create|list|show`.
func runRequest(ctx context.Context, inv invocation, stdout, stderr io.Writer) int {
	if len(inv.args) == 0 {
		fmt.Fprintf(stderr, "req: missing request command (want %q, %q or %q)\n%s", "create", "list", "show", requestUsage)
		return exitUsage
	}
	sub, rest := inv.args[0], inv.args[1:]
	subInv := invocation{args: rest, workspace: inv.workspace}
	switch sub {
	case "create":
		return requestCreate(ctx, subInv, stderr)
	case "list":
		return requestList(ctx, subInv, stdout, stderr)
	case "show":
		return requestShow(ctx, subInv, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "req: unknown request command %q (want %q, %q or %q)\n%s", sub, "create", "list", "show", requestUsage)
		return exitUsage
	}
}

// requestCreate implements `req request create PATH --method M --url U
// [-H Name:value]... [--query key=value]... [--body TEXT | --json JSON]
// [--parents]`. The URL is stored verbatim and may contain {{variables}}.
func requestCreate(ctx context.Context, inv invocation, stderr io.Writer) int {
	const createUsage = "usage: req request create PATH --method METHOD --url URL [-H Name:value]... [--query key=value]... [--body TEXT | --json JSON] [--parents]\n"

	var (
		positional          []string
		headers, queries    [][2]string
		method, rawURL      string
		haveMethod, haveURL bool
		bodyMode, bodyText  string
		parents             bool
	)
	value := func(i *int, name string) (string, error) {
		*i++
		if *i >= len(inv.args) {
			return "", fmt.Errorf("flag %s requires a value", name)
		}
		return inv.args[*i], nil
	}
	fail := func(format string, args ...interface{}) int {
		fmt.Fprintf(stderr, "req: "+format+"\n", args...)
		fmt.Fprint(stderr, createUsage)
		return exitUsage
	}
	for i := 0; i < len(inv.args); i++ {
		arg := inv.args[i]
		switch arg {
		case "--method":
			if haveMethod {
				return fail("%s", "--method given more than once")
			}
			v, err := value(&i, "--method")
			if err != nil {
				return fail("%v", err)
			}
			haveMethod, method = true, v
		case "--url":
			if haveURL {
				return fail("%s", "--url given more than once")
			}
			v, err := value(&i, "--url")
			if err != nil {
				return fail("%v", err)
			}
			haveURL, rawURL = true, v
		case "--header", "-H":
			v, err := value(&i, "--header")
			if err != nil {
				return fail("%v", err)
			}
			h, err := parseHeaderEntry(v)
			if err != nil {
				return fail("%v", err)
			}
			headers = append(headers, h)
		case "--query":
			v, err := value(&i, "--query")
			if err != nil {
				return fail("%v", err)
			}
			q, err := parseQueryEntry(v)
			if err != nil {
				return fail("%v", err)
			}
			queries = append(queries, q)
		case "--body":
			v, err := value(&i, "--body")
			if err != nil {
				return fail("%v", err)
			}
			if bodyMode != "" {
				return fail("%s", "--body conflicts with another body flag")
			}
			bodyMode, bodyText = "raw", v
		case "--json":
			v, err := value(&i, "--json")
			if err != nil {
				return fail("%v", err)
			}
			if bodyMode != "" {
				return fail("%s", "--json conflicts with another body flag")
			}
			// A {{reference}} may make the text valid JSON only after
			// substitution, so it is stored verbatim without validation.
			if !strings.Contains(v, "{{") && !json.Valid([]byte(v)) {
				return fail("%s", "--json value is not valid JSON")
			}
			bodyMode, bodyText = "json", v
		case "--parents":
			parents = true
		default:
			if strings.HasPrefix(arg, "-") && arg != "-" {
				return fail("unknown flag %q", arg)
			}
			positional = append(positional, arg)
		}
	}
	if len(positional) != 1 {
		return fail("request create needs exactly one PATH")
	}
	if !haveMethod && !haveURL {
		return fail("both --method and --url are required")
	}
	if !haveMethod {
		return fail("--method is required")
	}
	if !haveURL || rawURL == "" {
		return fail("--url is required")
	}
	method = strings.ToUpper(method)
	if !httpToken.MatchString(method) {
		return fail("invalid HTTP method %q", method)
	}

	req := model.Request{Method: method, URL: rawURL}
	for _, q := range queries {
		req.Query = append(req.Query, model.Entry{Key: q[0], Value: q[1], Enabled: true})
	}
	for _, h := range headers {
		req.Headers = append(req.Headers, model.Entry{Key: h[0], Value: h[1], Enabled: true})
	}
	if bodyMode != "" {
		req.Body = &model.Body{Type: bodyMode, Text: bodyText}
	}

	ws, code := openWorkspace(inv, stderr)
	if ws == nil {
		return code
	}
	path := positional[0]
	if err := ws.CreateRequest(ctx, path, req, parents); err != nil {
		fmt.Fprintf(stderr, "req: %v\n", err)
		return usageOrStorage(err)
	}
	fmt.Fprintf(stderr, "created request %q\n", path)
	return exitSuccess
}

// requestList implements `req request list PATH`: NAME<TAB>METHOD<TAB>URL per
// request-type child, in stored order, on stdout. Folders are not listed.
func requestList(ctx context.Context, inv invocation, stdout, stderr io.Writer) int {
	if len(inv.args) != 1 {
		fmt.Fprintf(stderr, "req: request list needs exactly one PATH\nusage: req request list PATH\n")
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
	children, code := folderChildren(rp, inv.args[0], stderr)
	if code != exitSuccess {
		return code
	}
	for _, it := range children {
		if it.Type != "request" {
			continue
		}
		fmt.Fprintf(stdout, "%s\t%s\t%s\n", it.Name, it.Request.Method, it.Request.URL)
	}
	return exitSuccess
}

// requestShow implements `req request show PATH`: the saved request as
// indented JSON on stdout.
func requestShow(ctx context.Context, inv invocation, stdout, stderr io.Writer) int {
	if len(inv.args) != 1 {
		fmt.Fprintf(stderr, "req: request show needs exactly one PATH\nusage: req request show PATH\n")
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
	switch {
	case rp.Item == nil:
		fmt.Fprintf(stderr, "req: %q names the collection, not a request\n", inv.args[0])
		return exitUsage
	case rp.Item.Type != "request":
		fmt.Fprintf(stderr, "req: %q is a folder, not a request\n", inv.args[0])
		return exitUsage
	}
	data, err := json.MarshalIndent(rp.Item.Request, "", "  ")
	if err != nil {
		fmt.Fprintf(stderr, "req: %v\n", err)
		return exitStorage
	}
	fmt.Fprintf(stdout, "%s\n", data)
	return exitSuccess
}

// folderChildren returns the child items a list/tree command operates on: at
// the collection root the collection's items, otherwise the folder's
// children. A path naming a request is a usage error.
func folderChildren(rp store.ResolvedPath, path string, stderr io.Writer) ([]model.Item, int) {
	if rp.Item == nil {
		return rp.Collection.Items, exitSuccess
	}
	if rp.Item.Type != "folder" {
		fmt.Fprintf(stderr, "req: %q is a request, not a folder\n", path)
		return nil, exitUsage
	}
	return rp.Item.Folder.Children, exitSuccess
}
