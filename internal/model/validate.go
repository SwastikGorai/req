package model

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
)

var (
	idPattern     = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	methodPattern = regexp.MustCompile(`^[!#$%&'*+\-.^_` + "`" + `|~0-9A-Za-z]+$`)
)

var authTypes = map[string]bool{"inherit": true, "none": true, "bearer": true, "basic": true}
var bodyTypes = map[string]bool{"none": true, "raw": true, "json": true, "urlencoded": true, "multipart": true}

// NewID returns a random identifier safe for filenames, prefixed by kind.
func NewID(kind string) string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("model: crypto/rand unavailable: " + err.Error())
	}
	return kind + "-" + hex.EncodeToString(b[:])
}

// ValidID reports whether id is a safe stable identifier/filename.
func ValidID(id string) bool { return idPattern.MatchString(id) }

// ValidName reports whether name is acceptable for collections, folders and
// requests: non-empty, not "." or "..", and free of path separators
// (both kinds — backslash is a separator on Windows).
func ValidName(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsAny(name, `/\`)
}

// SchemaVersionError reports a file whose schema_version is not the
// supported one. Such files fail without being rewritten.
type SchemaVersionError struct {
	Found, Want int
}

func (e *SchemaVersionError) Error() string {
	return fmt.Sprintf("unsupported schema_version %d (supported: %d)", e.Found, e.Want)
}

// Validate checks the full collection: schema version, identifiers, names,
// sibling-name and tree-wide ID uniqueness, auth/body tags and entry keys.
func (c *Collection) Validate() error {
	if c.SchemaVersion != SchemaVersion {
		return &SchemaVersionError{Found: c.SchemaVersion, Want: SchemaVersion}
	}
	if !ValidID(c.ID) {
		return fmt.Errorf("collection id %q is invalid (want 1-64 characters of [A-Za-z0-9_-])", c.ID)
	}
	if !ValidName(c.Name) {
		return fmt.Errorf("collection name %q is invalid", c.Name)
	}
	for name := range c.Variables {
		if name == "" {
			return fmt.Errorf("collection variable with an empty name")
		}
		if strings.HasPrefix(name, "env:") {
			return fmt.Errorf("collection variable %q uses the reserved env: prefix", name)
		}
	}
	if err := validateAuth(c.Auth, "collection auth"); err != nil {
		return err
	}
	if err := validateScripts(c.Scripts, "collection scripts"); err != nil {
		return err
	}
	return validateItems(c.Items, make(map[string]string))
}

func validateItems(items []Item, ids map[string]string) error {
	siblings := make(map[string]bool, len(items))
	for i, it := range items {
		where := fmt.Sprintf("items[%d] %q", i, it.Name)
		switch it.Type {
		case "folder", "request":
		default:
			return fmt.Errorf("%s: unknown item type %q (want folder or request)", where, it.Type)
		}
		if !ValidID(it.ID) {
			return fmt.Errorf("%s: invalid id %q", where, it.ID)
		}
		if !ValidName(it.Name) {
			return fmt.Errorf("%s: invalid name %q", where, it.Name)
		}
		if siblings[it.Name] {
			return fmt.Errorf("duplicate sibling name %q", it.Name)
		}
		siblings[it.Name] = true
		if prev, dup := ids[it.ID]; dup {
			return fmt.Errorf("duplicate id %q used by %q and %q", it.ID, prev, it.Name)
		}
		ids[it.ID] = it.Name

		if it.Type == "folder" {
			if it.Folder == nil || it.Request != nil {
				return fmt.Errorf("%s: folder item must carry exactly a folder payload", where)
			}
			if err := validateAuth(it.Folder.Auth, where+" auth"); err != nil {
				return err
			}
			if err := validateScripts(it.Folder.Scripts, where+" scripts"); err != nil {
				return err
			}
			if err := validateItems(it.Folder.Children, ids); err != nil {
				return err
			}
			continue
		}
		if it.Request == nil || it.Folder != nil {
			return fmt.Errorf("%s: request item must carry exactly a request payload", where)
		}
		r := it.Request
		if !methodPattern.MatchString(r.Method) {
			return fmt.Errorf("%s: invalid method %q", where, r.Method)
		}
		if r.URL == "" {
			return fmt.Errorf("%s: empty URL", where)
		}
		if err := validateEntries(r.Query, where+" query"); err != nil {
			return err
		}
		if err := validateEntries(r.Headers, where+" headers"); err != nil {
			return err
		}
		if err := validateAuth(r.Auth, where+" auth"); err != nil {
			return err
		}
		if err := validateBody(r.Body, where+" body"); err != nil {
			return err
		}
		if err := validateScripts(r.Scripts, where+" scripts"); err != nil {
			return err
		}
	}
	return nil
}

func validateEntries(entries []Entry, where string) error {
	for i, e := range entries {
		if e.Key == "" {
			return fmt.Errorf("%s[%d]: empty key", where, i)
		}
	}
	return nil
}

func validateAuth(a *Auth, where string) error {
	if a == nil {
		return nil // not set: inherit
	}
	if !authTypes[a.Type] {
		return fmt.Errorf("%s: unknown auth type %q (want inherit, none, bearer or basic)", where, a.Type)
	}
	return nil
}

func validateBody(b *Body, where string) error {
	if b == nil {
		return nil
	}
	if !bodyTypes[b.Type] {
		return fmt.Errorf("%s: unknown body type %q (want none, raw, json, urlencoded or multipart)", where, b.Type)
	}
	if b.Text != "" && b.File != "" {
		return fmt.Errorf("%s: inline text and file reference are mutually exclusive", where)
	}
	if err := validateEntries(b.URLEncoded, where+" urlencoded"); err != nil {
		return err
	}
	for i, f := range b.Multipart {
		if f.Key == "" {
			return fmt.Errorf("%s multipart[%d]: empty key", where, i)
		}
		if f.Value != "" && f.File != "" {
			return fmt.Errorf("%s multipart[%d]: value and file are mutually exclusive", where, i)
		}
	}
	return nil
}

func validateScripts(s *Scripts, where string) error {
	if s == nil {
		return nil
	}
	ids := make(map[string]bool, len(s.PreRequest)+len(s.PostResponse))
	for i, script := range append(append([]Script(nil), s.PreRequest...), s.PostResponse...) {
		if !ValidID(script.ID) {
			return fmt.Errorf("%s[%d]: invalid script id %q", where, i, script.ID)
		}
		if ids[script.ID] {
			return fmt.Errorf("%s: duplicate script id %q", where, script.ID)
		}
		ids[script.ID] = true
	}
	return nil
}
