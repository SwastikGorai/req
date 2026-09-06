package model

import (
	"errors"
	"strings"
	"testing"
)

func validCollection() Collection {
	return Collection{
		SchemaVersion: 1,
		ID:            "col-val-001",
		Name:          "Valid",
		Variables:     map[string]interface{}{"base_url": "https://x.test"},
		Items: []Item{
			{Type: "request", ID: "req-a", Name: "A", Request: &Request{Method: "GET", URL: "{{base_url}}/a"}},
		},
	}
}

func TestValidateAccepts(t *testing.T) {
	c := validCollection()
	if err := c.Validate(); err != nil {
		t.Fatalf("valid collection rejected: %v", err)
	}
}

func TestValidateRejections(t *testing.T) {
	mutate := func(f func(*Collection)) Collection {
		c := validCollection()
		f(&c)
		return c
	}
	cases := map[string]Collection{
		"future schema": mutate(func(c *Collection) { c.SchemaVersion = 2 }),
		"old schema":    mutate(func(c *Collection) { c.SchemaVersion = 0 }),
		"empty id":      mutate(func(c *Collection) { c.ID = "" }),
		"unsafe id":     mutate(func(c *Collection) { c.ID = "../escape" }),
		"empty name":    mutate(func(c *Collection) { c.Name = "" }),
		"slash name":    mutate(func(c *Collection) { c.Name = "a/b" }),
		"dot name":      mutate(func(c *Collection) { c.Name = "." }),
		"dotdot name":   mutate(func(c *Collection) { c.Name = ".." }),
		"backslash name": mutate(func(c *Collection) { c.Name = `a\b` }),
		"reserved env var": mutate(func(c *Collection) { c.Variables["env:HOME"] = "x" }),
		"bad auth type": mutate(func(c *Collection) { c.Auth = &Auth{Type: "oauth2"} }),
		"bad body type": mutate(func(c *Collection) {
			c.Items[0].Request.Body = &Body{Type: "graphql"}
		}),
		"raw both text and file": mutate(func(c *Collection) {
			c.Items[0].Request.Body = &Body{Type: "raw", Text: "a", File: "b.txt"}
		}),
		"multipart both value and file": mutate(func(c *Collection) {
			c.Items[0].Request.Body = &Body{Type: "multipart", Multipart: []MultipartField{{Key: "f", Value: "v", File: "g.txt"}}}
		}),
		"empty entry key": mutate(func(c *Collection) {
			c.Items[0].Request.Headers = []Entry{{Key: "", Value: "v"}}
		}),
		"bad method": mutate(func(c *Collection) { c.Items[0].Request.Method = "GE T" }),
		"empty URL":  mutate(func(c *Collection) { c.Items[0].Request.URL = "" }),
		"unknown item type": mutate(func(c *Collection) {
			c.Items[0].Type = "websocket"
		}),
		"folder item with request payload": mutate(func(c *Collection) {
			c.Items[0] = Item{Type: "folder", ID: "f1", Name: "F", Request: &Request{Method: "GET", URL: "https://x.test"}}
		}),
		"request item without payload": mutate(func(c *Collection) {
			c.Items[0] = Item{Type: "request", ID: "r1", Name: "R"}
		}),
		"duplicate sibling names": mutate(func(c *Collection) {
			c.Items = append(c.Items, Item{Type: "folder", ID: "fld-1", Name: "A", Folder: &Folder{}})
		}),
		"duplicate ids across tree": mutate(func(c *Collection) {
			nested := Item{Type: "request", ID: "req-a", Name: "Nested", Request: &Request{Method: "GET", URL: "https://x.test"}}
			c.Items = append(c.Items, Item{Type: "folder", ID: "fld-1", Name: "F", Folder: &Folder{Children: []Item{nested}}})
		}),
		"duplicate script ids": mutate(func(c *Collection) {
			c.Items[0].Request.Scripts = &Scripts{
				PreRequest:   []Script{{ID: "scr-x", Source: "//", Enabled: true}},
				PostResponse: []Script{{ID: "scr-x", Source: "//", Enabled: true}},
			}
		}),
		"invalid nested name": mutate(func(c *Collection) {
			c.Items = append(c.Items, Item{Type: "folder", ID: "fld-2", Name: "F", Folder: &Folder{Children: []Item{
				{Type: "request", ID: "req-b", Name: "x/y", Request: &Request{Method: "GET", URL: "https://x.test"}},
			}}})
		}),
	}
	for name, c := range cases {
		if err := c.Validate(); err == nil {
			t.Errorf("%s: accepted, want rejection", name)
		}
	}

	var serr *SchemaVersionError
	c := mutate(func(c *Collection) { c.SchemaVersion = 99 })
	if err := c.Validate(); !errors.As(err, &serr) || serr.Found != 99 {
		t.Errorf("future schema: got %v, want SchemaVersionError{Found:99}", err)
	}
}

func TestValidName(t *testing.T) {
	for _, ok := range []string{"Login", "Sign in", "v2 users", "a.b", "深い"} {
		if !ValidName(ok) {
			t.Errorf("ValidName(%q) = false, want true", ok)
		}
	}
	for _, bad := range []string{"", ".", "..", "a/b", `a\b`, "/x", "x/"} {
		if ValidName(bad) {
			t.Errorf("ValidName(%q) = true, want false", bad)
		}
	}
}

func TestNewID(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		id := NewID("col")
		if !strings.HasPrefix(id, "col-") || !ValidID(id) {
			t.Fatalf("NewID produced %q", id)
		}
		if seen[id] {
			t.Fatalf("NewID repeated %q after %d draws", id, i)
		}
		seen[id] = true
	}
}
