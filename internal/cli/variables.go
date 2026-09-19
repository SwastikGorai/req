package cli

import (
	"context"
	"fmt"
	"req/internal/model"
	"req/internal/store"
	"req/internal/variables"
	"strings"
)

type variableFlags struct {
	env            string
	envRevision    store.Revision
	values         map[string]any
	auth           *model.Auth
	user, password bool
}

func (f *variableFlags) parse(args []string, i *int) (bool, error) {
	flag := args[*i]
	switch flag {
	case "--env", "--var", "--bearer", "--basic-user", "--basic-password", "--no-auth":
	default:
		return false, nil
	}
	v := ""
	if flag != "--no-auth" {
		*i++
		if *i >= len(args) {
			return true, fmt.Errorf("%s requires a value", flag)
		}
		v = args[*i]
	}
	switch flag {
	case "--env":
		if f.env != "" || !model.ValidEnvironmentName(v) {
			return true, fmt.Errorf("invalid or repeated --env")
		}
		f.env = v
	case "--var":
		key, val, ok := strings.Cut(v, "=")
		if !ok {
			return true, fmt.Errorf("--var needs key=value")
		}
		if f.values == nil {
			f.values = map[string]any{}
		}
		f.values[key] = val
		if err := model.ValidateVariables(f.values); err != nil {
			return true, err
		}
	default:
		kind := "basic"
		if flag == "--bearer" {
			kind = "bearer"
		} else if flag == "--no-auth" {
			kind = "none"
		}
		if f.auth != nil && (f.auth.Type != kind || kind != "basic") {
			return true, fmt.Errorf("auth flags are mutually exclusive")
		}
		if f.auth == nil {
			f.auth = &model.Auth{Type: kind}
		}
		switch flag {
		case "--bearer":
			f.auth.Token = v
		case "--basic-user":
			if f.user {
				return true, fmt.Errorf("repeated --basic-user")
			}
			f.user, f.auth.Username = true, v
		case "--basic-password":
			if f.password {
				return true, fmt.Errorf("repeated --basic-password")
			}
			f.password, f.auth.Password = true, v
		}
	}
	return true, nil
}

func (f variableFlags) validate() error {
	if f.user != f.password {
		return fmt.Errorf("--basic-user and --basic-password must be supplied together")
	}
	return nil
}

func (f *variableFlags) scope(ctx context.Context, ws *store.Workspace, collection map[string]any) (*variables.Scope, error) {
	if collection == nil {
		collection = map[string]any{}
	}
	s := &variables.Scope{CLI: f.values, Local: map[string]any{}, Collection: collection}
	if f.env != "" {
		e, rev, err := ws.LoadEnvironment(ctx, f.env)
		if err != nil {
			return nil, err
		}
		f.envRevision = rev
		s.Environment = e.ActiveVariables()
		if s.Environment == nil {
			s.Environment = map[string]any{}
		}
	}
	return s, nil
}
