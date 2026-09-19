package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/SwastikGorai/req/internal/model"
)

// scriptUsage is the usage line of the script command family.
const scriptUsage = "usage: req script edit PATH (--pre|--post)\n"

// scriptMarkerPattern matches exactly one separator line identifying a
// stored script entry by its ID.
var scriptMarkerPattern = regexp.MustCompile(`^// ---- req script ([A-Za-z0-9_-]{1,64}) ----$`)

// runScript implements `req script edit PATH (--pre|--post)`.
func runScript(ctx context.Context, inv invocation, stdout, stderr io.Writer) int {
	if len(inv.args) == 0 {
		fmt.Fprintf(stderr, "req: missing script command (want %q)\n%s", "edit", scriptUsage)
		return exitUsage
	}
	sub, rest := inv.args[0], inv.args[1:]
	if sub != "edit" {
		fmt.Fprintf(stderr, "req: unknown script command %q (want %q)\n%s", sub, "edit", scriptUsage)
		return exitUsage
	}
	return scriptEdit(ctx, invocation{args: rest, workspace: inv.workspace}, stderr)
}

// scriptEdit renders one script phase of the node at PATH (collection root,
// folder or request) as a single editable source text, opens it in
// $EDITOR/$VISUAL and stores it back under the source revision. The marker
// lines identifying the stored entries must survive the edit unchanged in
// count, order and values.
func scriptEdit(ctx context.Context, inv invocation, stderr io.Writer) int {
	var (
		positional []string
		phase      string
	)
	for _, arg := range inv.args {
		switch arg {
		case "--pre", "--post":
			if phase != "" {
				fmt.Fprintf(stderr, "req: --pre and --post are mutually exclusive\n%s", scriptUsage)
				return exitUsage
			}
			phase = arg[2:]
		default:
			if strings.HasPrefix(arg, "-") && arg != "-" {
				fmt.Fprintf(stderr, "req: unknown flag %q\n%s", arg, scriptUsage)
				return exitUsage
			}
			positional = append(positional, arg)
		}
	}
	if len(positional) != 1 || phase == "" {
		fmt.Fprintf(stderr, "req: script edit needs exactly one PATH and exactly one of --pre or --post\n%s", scriptUsage)
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

	// The node's current scripts; ResolvePath accepts all three shapes.
	var node *model.Scripts
	switch {
	case rp.Item == nil:
		node = rp.Collection.Scripts
	case rp.Item.Type == "folder":
		node = rp.Item.Folder.Scripts
	default:
		node = rp.Item.Request.Scripts
	}
	var current []model.Script
	if node != nil {
		if phase == "pre" {
			current = node.PreRequest
		} else {
			current = node.PostResponse
		}
	}

	// save replaces only the edited phase; UpdateScripts stores nil when
	// both arrays end up empty.
	var updated []model.Script
	save := func() error {
		s := &model.Scripts{}
		if node != nil {
			s.PreRequest, s.PostResponse = node.PreRequest, node.PostResponse
		}
		if phase == "pre" {
			s.PreRequest = updated
		} else {
			s.PostResponse = updated
		}
		return ws.UpdateScripts(ctx, path, s, rp.Rev)
	}
	return launchEdit(ws, rp.Collection.ID, fmt.Sprintf("scripts %q", path), "req-script-*.js", renderScripts(current),
		func(edited []byte) error {
			var err error
			updated, err = parseScripts(string(edited), current)
			return err
		},
		save, stderr)
}

// renderScripts renders entries into one editable source text: a marker line
// per entry followed by its source with one trailing newline normalized; an
// empty source is just its marker line. Zero entries render as an empty file.
func renderScripts(entries []model.Script) []byte {
	var b strings.Builder
	for _, e := range entries {
		b.WriteString("// ---- req script ")
		b.WriteString(e.ID)
		b.WriteString(" ----\n")
		if source := strings.TrimSuffix(e.Source, "\n"); source != "" {
			b.WriteString(source)
			b.WriteByte('\n')
		}
	}
	return []byte(b.String())
}

// parseScripts splits the edited text back into script entries. Lines under a
// marker line form that entry's source; every non-empty source ends up with
// exactly one trailing newline (a stored source without one gains it, which
// JavaScript does not care about).
// The marker ID sequence must equal want exactly (count, order, values); a
// missing, extra, reordered or damaged separator fails. A non-empty text with
// no separators creates one new enabled entry — but only when want is empty.
func parseScripts(text string, want []model.Script) ([]model.Script, error) {
	lines := strings.Split(text, "\n")
	first := -1
	for i, line := range lines {
		if strings.TrimSpace(line) != "" {
			first = i
			break
		}
	}
	if first < 0 { // no content at all
		if len(want) == 0 {
			return nil, nil
		}
		return nil, errors.New("script separator missing, damaged or reordered")
	}
	if !scriptMarkerPattern.MatchString(lines[first]) {
		if len(want) != 0 {
			return nil, errors.New("script separator missing, damaged or reordered")
		}
		// A marker anywhere else would be stored as source text and break the
		// next edit's parsing, so refuse it up front.
		for _, line := range lines[first:] {
			if scriptMarkerPattern.MatchString(line) {
				return nil, errors.New("unexpected script separator (this phase has no entries yet)")
			}
		}
		return []model.Script{{ID: model.NewID("script"), Enabled: true, Source: text}}, nil
	}

	var entries []model.Script
	var group []string
	flush := func() {
		if len(group) > 0 && group[len(group)-1] == "" {
			group = group[:len(group)-1] // the newline the rendering added
		}
		if len(group) > 0 {
			entries[len(entries)-1].Source = strings.Join(group, "\n") + "\n"
		}
		group = group[:0]
	}
	for _, line := range lines[first:] {
		if m := scriptMarkerPattern.FindStringSubmatch(line); m != nil {
			if len(entries) > 0 {
				flush()
			}
			entries = append(entries, model.Script{ID: m[1]})
			continue
		}
		group = append(group, line)
	}
	flush()

	if len(entries) != len(want) {
		return nil, errors.New("script separator missing, damaged or reordered")
	}
	for i := range entries {
		if entries[i].ID != want[i].ID {
			return nil, errors.New("script separator missing, damaged or reordered")
		}
		// The editable text carries only sources: enabled state and
		// provenance survive from the stored entries.
		entries[i].Enabled = want[i].Enabled
		entries[i].Provenance = want[i].Provenance
	}
	return entries, nil
}
