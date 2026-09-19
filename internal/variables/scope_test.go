package variables

import (
	"strings"
	"testing"
)

func TestVariablePrecedence(t *testing.T) {
	s := &Scope{CLI: map[string]any{"v": "cli"}, Local: map[string]any{"v": "local"}, Environment: map[string]any{"v": "env"}, Collection: map[string]any{"v": "collection", "null": nil}}
	for _, step := range []struct {
		want  string
		layer map[string]any
	}{{"cli", s.CLI}, {"local", s.Local}, {"env", s.Environment}, {"collection", s.Collection}} {
		if got, _ := s.Get("v"); got != step.want {
			t.Fatalf("got %v, want %s", got, step.want)
		}
		delete(step.layer, "v")
	}
	if _, ok := s.Get("v"); ok {
		t.Fatal("missing key exists")
	}
	if v, ok := s.Get("null"); !ok || v != nil {
		t.Fatal("null treated as missing")
	}
}

func TestSinglePassVariables(t *testing.T) {
	t.Setenv("REQ_TEST_VALUE", "process")
	s := &Scope{Collection: map[string]any{"n": 2, "object": map[string]any{"a": true}, "cycle": "{{n}}"}}
	got, err := s.ResolveString(`{{n}}/{{object}}/{{env:REQ_TEST_VALUE}}`, "body")
	if err != nil || got != `2/{"a":true}/process` {
		t.Fatalf("%s: %v", got, err)
	}
	for _, value := range []string{"{{missing}}", "{{cycle}}", "{{env:REQ_TEST_UNSET}}"} {
		if _, err := s.ResolveString(value, "body"); err == nil || !strings.Contains(err.Error(), "body") {
			t.Fatalf("%q: %v", value, err)
		}
	}
}

func TestPersistedChangesTrackOnlyPersistentLayers(t *testing.T) {
	s := &Scope{CLI: map[string]any{"cli": "original"}, Local: map[string]any{}}
	s.SetEnvironment("token", "fresh")
	s.SetCollection("base", "https://example.test")
	s.UnsetCollection("old")
	if len(s.EnvironmentChanges()) != 1 || s.EnvironmentChanges()["token"].Value != "fresh" {
		t.Fatalf("environment changes = %#v", s.EnvironmentChanges())
	}
	changes := s.CollectionChanges()
	if len(changes) != 2 || changes["old"].Unset != true || changes["base"].Value != "https://example.test" {
		t.Fatalf("collection changes = %#v", changes)
	}
	if len(s.Local) != 0 || s.CLI["cli"] != "original" {
		t.Fatal("persistent change tracking touched local or CLI layers")
	}
}

func TestReplaceIn(t *testing.T) {
	t.Setenv("REQ_TEST_VALUE", "process")
	s := &Scope{CLI: map[string]any{"v": "cli"}, Collection: map[string]any{"n": 2}}
	for _, step := range []struct{ in, want string }{
		{"{{v}}", "cli"},
		{"{{n}}", "2"},
		{"{{env:REQ_TEST_VALUE}}", "process"},
		{"a {{missing}} b", "a {{missing}} b"}, // unresolvable placeholders stay
		{"{{v}} and {{v}}", "cli and cli"},
		{"", ""},
	} {
		if got := s.ReplaceIn(step.in); got != step.want {
			t.Errorf("ReplaceIn(%q) = %q, want %q", step.in, got, step.want)
		}
	}
}
