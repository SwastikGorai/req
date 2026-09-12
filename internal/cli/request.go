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

const requestUsage = `usage: req request create PATH --method METHOD --url URL [flags]
       req request list PATH
       req request show PATH
       req request rename PATH NEW
       req request move SRC DEST
       req request delete PATH [--yes]
       req request edit PATH
`

// runRequest implements `req request create|list|show|rename|move|delete|edit`.
func runRequest(ctx context.Context, inv invocation, stdout, stderr io.Writer) int {
	if len(inv.args) == 0 {
		fmt.Fprintf(stderr, "req: missing request command (want %q, %q, %q, %q, %q, %q or %q)\n%s",
			"create", "list", "show", "rename", "move", "delete", "edit", requestUsage)
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
	case "rename":
		return renameItem(ctx, subInv, "request rename", stderr)
	case "move":
		return moveItem(ctx, subInv, "request move", stderr)
	case "delete":
		return requestDelete(ctx, subInv, stderr)
	case "edit":
		return editRequest(ctx, subInv, stderr)
	default:
		fmt.Fprintf(stderr, "req: unknown request command %q (want %q, %q, %q, %q, %q, %q or %q)\n%s",
			sub, "create", "list", "show", "rename", "move", "delete", "edit", requestUsage)
		return exitUsage
	}
}

// requestCreate implements `req request create PATH --method M --url U
// [-H Name:value]... [--query key=value]... [body flags]
// [--parents]`. The URL is stored verbatim and may contain {{variables}}.
func requestCreate(ctx context.Context, inv invocation, stderr io.Writer) int {
	const createUsage = "usage: req request create PATH --method METHOD --url URL [header/query/body/auth flags] [--parents] (see req help)\n"

	var (
		positional          []string
		headers, queries    [][2]string
		method, rawURL      string
		haveMethod, haveURL bool
		body                *model.Body
		parents             bool
		authFlags           variableFlags
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
		case "--body", "--body-file", "--json", "--form", "--form-file", "--urlencoded":
			v, err := value(&i, arg)
			if err != nil {
				return fail("%v", err)
			}
			if err := parseBodyFlag(&body, arg, v); err != nil {
				return fail("%v", err)
			}
		case "--parents":
			parents = true
		case "--bearer", "--basic-user", "--basic-password", "--no-auth":
			if _, err := authFlags.parse(inv.args, &i); err != nil {
				return fail("%v", err)
			}
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

	if err := authFlags.validate(); err != nil {
		return fail("%v", err)
	}
	req := model.Request{Method: method, URL: rawURL, Auth: authFlags.auth}
	for _, q := range queries {
		req.Query = append(req.Query, model.Entry{Key: q[0], Value: q[1], Enabled: true})
	}
	for _, h := range headers {
		req.Headers = append(req.Headers, model.Entry{Key: h[0], Value: h[1], Enabled: true})
	}
	req.Body = body

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

// requestDelete implements `req request delete PATH [--yes]`. --yes is
// accepted for symmetry with the collection and folder deletes, but a
// request is a leaf and needs no confirmation.
func requestDelete(ctx context.Context, inv invocation, stderr io.Writer) int {
	const deleteUsage = "usage: req request delete PATH [--yes]\n"

	var (
		positional []string
		yes        bool
	)
	for _, arg := range inv.args {
		switch arg {
		case "--yes":
			yes = true
		default:
			if strings.HasPrefix(arg, "-") && arg != "-" {
				fmt.Fprintf(stderr, "req: unknown flag %q\n%s", arg, deleteUsage)
				return exitUsage
			}
			positional = append(positional, arg)
		}
	}
	_ = yes // accepted but unused: leaf deletes need no confirmation
	if len(positional) != 1 {
		fmt.Fprintf(stderr, "req: request delete needs exactly one PATH\n%s", deleteUsage)
		return exitUsage
	}
	path := positional[0]
	ws, code := openWorkspace(inv, stderr)
	if ws == nil {
		return code
	}
	rp, err := ws.ResolvePath(ctx, path)
	if err != nil {
		fmt.Fprintf(stderr, "req: %v\n", err)
		return usageOrStorage(err)
	}
	switch {
	case rp.Item == nil:
		fmt.Fprintf(stderr, "req: %q names the collection (use `req collection delete %s` to delete it)\n", path, path)
		return exitUsage
	case rp.Item.Type != "request":
		fmt.Fprintf(stderr, "req: %q is a folder, not a request (use `req folder delete %s` to delete it)\n", path, path)
		return exitUsage
	}
	if err := ws.DeleteItem(ctx, path); err != nil {
		fmt.Fprintf(stderr, "req: %v\n", err)
		return usageOrStorage(err)
	}
	fmt.Fprintf(stderr, "deleted request %q\n", path)
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
