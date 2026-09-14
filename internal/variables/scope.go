package variables

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
)

type Scope struct {
	CLI, Local, Environment, Collection map[string]any
}

func (s *Scope) Get(name string) (any, bool) {
	if s != nil {
		for _, layer := range []map[string]any{s.CLI, s.Local, s.Environment, s.Collection} {
			if value, ok := layer[name]; ok {
				return value, true
			}
		}
	}
	return nil, false
}

var reference = regexp.MustCompile(`\{\{([^{}]*)\}\}`)

// lookup resolves one reference key the way substitution does: env:NAME
// reads one process environment variable, anything else walks the scope
// chain.
func (s *Scope) lookup(key string) (any, bool) {
	value, ok := s.Get(key)
	if name, isEnv := strings.CutPrefix(key, "env:"); isEnv {
		value, ok = os.LookupEnv(name)
	}
	return value, ok
}

// ResolveString substitutes every {{reference}} and fails when one remains
// unresolved afterwards.
func (s *Scope) ResolveString(text, location string) (string, error) {
	var failure error
	result := reference.ReplaceAllStringFunc(text, func(match string) string {
		key := match[2 : len(match)-2]
		value, ok := s.lookup(key)
		if !ok {
			if failure == nil {
				failure = fmt.Errorf("unresolved variable %s in %s", match, location)
			}
			return match
		}
		if str, ok := value.(string); ok {
			return str
		}
		encoded, err := json.Marshal(value)
		if err != nil && failure == nil {
			failure = fmt.Errorf("variable %q in %s: %w", key, location, err)
		}
		return string(encoded)
	})
	if failure != nil {
		return "", failure
	}
	if remaining := reference.FindString(result); remaining != "" {
		return "", fmt.Errorf("unresolved variable %s in %s after single-pass substitution", remaining, location)
	}
	return result, nil
}

// ReplaceIn substitutes every resolvable {{reference}}, leaving unresolved
// placeholders intact. It is the pm.variables.replaceIn primitive.
func (s *Scope) ReplaceIn(text string) string {
	return reference.ReplaceAllStringFunc(text, func(match string) string {
		key := match[2 : len(match)-2]
		value, ok := s.lookup(key)
		if !ok {
			return match
		}
		if str, ok := value.(string); ok {
			return str
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			return match
		}
		return string(encoded)
	})
}
