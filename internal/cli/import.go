package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"req/internal/importer"
	"req/internal/store"
)

const importUsage = `usage: req import postman FILE [--name NAME] [--strict]
       req import postman-env FILE [--name NAME] [--strict]
`

// runImport imports one Postman collection or basic environment. Parsing is
// completed before importer persistence, so strict/lossy failures cannot leave
// a partially converted file behind.
func runImport(ctx context.Context, inv invocation, stdout, stderr io.Writer) int {
	if len(inv.args) == 0 {
		fmt.Fprint(stderr, importUsage)
		return exitUsage
	}
	kind := inv.args[0]
	var (
		positional []string
		name       string
		strict     bool
		nameSet    bool
	)
	for i := 1; i < len(inv.args); i++ {
		arg := inv.args[i]
		switch {
		case arg == "--strict":
			strict = true
		case arg == "--name":
			if i+1 >= len(inv.args) {
				fmt.Fprintf(stderr, "req: --name requires a value\n%s", importUsage)
				return exitUsage
			}
			i++
			if nameSet {
				fmt.Fprintln(stderr, "req: --name given more than once")
				return exitUsage
			}
			name = inv.args[i]
			nameSet = true
		case strings.HasPrefix(arg, "--name="):
			if nameSet {
				fmt.Fprintln(stderr, "req: --name given more than once")
				return exitUsage
			}
			name = strings.TrimPrefix(arg, "--name=")
			nameSet = true
		default:
			if strings.HasPrefix(arg, "-") && arg != "-" {
				fmt.Fprintf(stderr, "req: unknown flag %q\n%s", arg, importUsage)
				return exitUsage
			}
			positional = append(positional, arg)
		}
	}
	if len(positional) != 1 || (kind != "postman" && kind != "postman-env") {
		if kind != "postman" && kind != "postman-env" {
			fmt.Fprintf(stderr, "req: unknown import type %q\n%s", kind, importUsage)
		} else {
			fmt.Fprintf(stderr, "req: %s needs exactly one FILE\n%s", kind, importUsage)
		}
		return exitUsage
	}
	if nameSet && name == "" {
		fmt.Fprintln(stderr, "req: --name requires a non-empty value")
		return exitUsage
	}
	ws, code := openWorkspace(inv, stderr)
	if ws == nil {
		return code
	}
	source := filepath.Clean(positional[0])
	data, err := os.ReadFile(source)
	if err != nil {
		fmt.Fprintf(stderr, "req: reading import %q: %v\n", positional[0], err)
		return exitUsage
	}
	opts := importer.Options{Strict: strict, Name: name, Source: source}
	switch kind {
	case "postman":
		result, importErr := importer.ImportPostmanCollection(ctx, ws, data, opts)
		if importErr != nil {
			printImportError(stderr, importErr)
			return importErrorCode(importErr)
		}
		printWarnings(stderr, result.Warnings)
		fmt.Fprintf(stderr, "imported collection %q from %s: %d folders, %d requests, %d scripts, %d variables\n", result.Collection.Name, source, result.Counts.Folders, result.Counts.Requests, result.Counts.Scripts, result.Counts.Variables)
	default:
		result, importErr := importer.ImportPostmanEnvironment(ctx, ws, data, opts)
		if importErr != nil {
			printImportError(stderr, importErr)
			return importErrorCode(importErr)
		}
		printWarnings(stderr, result.Warnings)
		fmt.Fprintf(stderr, "imported environment %q from %s: %d variables\n", result.Environment.Name, source, result.Counts.Variables)
	}
	return exitSuccess
}

func printWarnings(w io.Writer, warnings []importer.Warning) {
	for _, warning := range importer.SortedWarnings(warnings) {
		if warning.Path == "" {
			fmt.Fprintf(w, "warning [%s]: %s\n", warning.Code, warning.Message)
		} else {
			fmt.Fprintf(w, "warning [%s] %s: %s\n", warning.Code, warning.Path, warning.Message)
		}
	}
}

func printImportError(w io.Writer, err error) {
	var strict *importer.StrictError
	if errors.As(err, &strict) {
		printWarnings(w, strict.Warnings)
	}
	fmt.Fprintf(w, "req: %v\n", err)
}

func importErrorCode(err error) int {
	var strict *importer.StrictError
	if errors.As(err, &strict) || errors.Is(err, store.ErrDuplicateName) {
		return exitUsage
	}
	var conflict *store.ConflictError
	if errors.As(err, &conflict) {
		return exitStorage
	}
	if strings.HasPrefix(err.Error(), "postman ") {
		return exitUsage
	}
	return exitStorage
}
