package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"req/internal/model"
	"req/internal/store"
)

func runEnv(ctx context.Context, inv invocation, stdout, stderr io.Writer) int {
	if len(inv.args) == 0 {
		fmt.Fprintln(stderr, "usage: req env create|list|edit|delete [NAME]")
		return exitUsage
	}
	command := inv.args[0]
	if command != "list" && command != "create" && command != "edit" && command != "delete" {
		fmt.Fprintln(stderr, "req: unknown env command")
		return exitUsage
	}
	name := ""
	if command == "list" {
		if len(inv.args) != 1 {
			fmt.Fprintln(stderr, "req: env list takes no arguments")
			return exitUsage
		}
	} else {
		if len(inv.args) != 2 || !model.ValidEnvironmentName(inv.args[1]) {
			fmt.Fprintln(stderr, "req: environment requires a safe NAME (1-64 ASCII letters, digits, _ or -; no device names)")
			return exitUsage
		}
		name = inv.args[1]
	}
	ws, code := openWorkspace(inv, stderr)
	if ws == nil {
		return code
	}
	if command == "list" {
		envs, err := ws.ListEnvironments(ctx)
		if err != nil {
			fmt.Fprintf(stderr, "req: %v\n", err)
			return usageOrStorage(err)
		}
		for _, e := range envs {
			fmt.Fprintln(stdout, e.Name)
		}
		return exitSuccess
	}
	e, rev, err := ws.LoadEnvironment(ctx, name)
	if command == "create" {
		if err == nil {
			fmt.Fprintf(stderr, "req: environment %q already exists\n", name)
			return exitUsage
		}
		if !errors.Is(err, store.ErrNotFound) {
			fmt.Fprintf(stderr, "req: %v\n", err)
			return usageOrStorage(err)
		}
		err = ws.SaveEnvironment(ctx, model.Environment{SchemaVersion: model.SchemaVersion, Name: name, Variables: map[string]any{}}, "")
	} else {
		if err != nil {
			fmt.Fprintf(stderr, "req: %v\n", err)
			return usageOrStorage(err)
		}
		if command == "edit" {
			var edited model.Environment
			return editJSON(ws, "env-"+name, fmt.Sprintf("environment %q", name), e, &edited, func() error {
				if edited.Name != name {
					return fmt.Errorf("environment edit cannot rename identity: %w", store.ErrInvalidPath)
				}
				if err := edited.Validate(); err != nil {
					return fmt.Errorf("%v: %w", err, store.ErrInvalidPath)
				}
				return ws.SaveEnvironment(ctx, edited, rev)
			}, stderr)
		}
		err = ws.DeleteEnvironment(ctx, name, rev)
	}
	if err != nil {
		fmt.Fprintf(stderr, "req: %v\n", err)
		return usageOrStorage(err)
	}
	fmt.Fprintf(stderr, "%s environment %q\n", command, name)
	return exitSuccess
}
