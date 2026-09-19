package execution

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SwastikGorai/req/internal/model"
	"github.com/SwastikGorai/req/internal/variables"
)

func TestBodyResolution(t *testing.T) {
	value := "{{value}}"
	saved := model.Request{Method: "POST", URL: "http://localhost", Body: &model.Body{Type: "urlencoded", URLEncoded: []model.Entry{
		{Key: "q", Value: value, Enabled: true}, {Key: "q", Value: "", Enabled: true}, {Key: "other", Value: "{{missing}}"},
	}}}
	before, _ := json.Marshal(saved)
	o, err := Prepare(saved, Overrides{}, Policy{Variables: &variables.Scope{CLI: map[string]any{"value": "a b&c"}}})
	if err != nil {
		t.Fatal(err)
	}
	r, err := o.Body.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	data, _ := io.ReadAll(r)
	if string(data) != "q=a+b%26c&q=" {
		t.Fatalf("%s", data)
	}
	after, _ := json.Marshal(saved)
	if string(before) != string(after) {
		t.Fatal("saved body mutated")
	}
	for _, raw := range []string{`{"type":"raw","text":"","file":"x"}`, `{"type":"multipart","multipart":[{"key":"x","value":"","file":"x"}]}`, `{"type":"none","text":""}`, `{"type":"raw","urlencoded":[]}`} {
		var b model.Body
		if err := json.Unmarshal([]byte(raw), &b); err != nil {
			t.Fatal(err)
		}
		if err := b.Validate(); err == nil {
			t.Fatalf("invalid body accepted: %s", raw)
		}
	}
}

func TestUntrustedBodyPaths(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "safe.json")
	if err := os.WriteFile(path, []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{path, "../outside.json"} {
		if _, err := bodyFilePath(path, root, true); err == nil || !strings.Contains(err.Error(), "remap") {
			t.Fatalf("%q: %v", path, err)
		}
	}
	resolved, err := bodyFilePath("safe.json", root, true)
	if err != nil || resolved != path {
		t.Fatalf("%q: %v", resolved, err)
	}
	outside := filepath.Join(t.TempDir(), "outside.json")
	if err := os.WriteFile(outside, []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link.json")
	if err := os.Symlink(outside, link); err != nil {
		t.Logf("symlink runtime check unavailable: %v", err)
		return
	}
	if _, err := bodyFilePath("link.json", root, true); err == nil {
		t.Fatal("symlink escape accepted")
	}
}

func TestEmptyAndAbsentBody(t *testing.T) {
	for _, raw := range []string{`null`, `{"type":"none"}`, `{"type":"raw"}`, `{"type":"raw","text":""}`} {
		var body *model.Body
		if err := json.Unmarshal([]byte(raw), &body); err != nil {
			t.Fatal(err)
		}
		o, err := Prepare(model.Request{Method: "POST", URL: "http://localhost", Body: body}, Overrides{}, Policy{})
		if err != nil {
			t.Fatal(err)
		}
		absent := body == nil || body.Type == "none"
		if (o.Body == nil) != absent {
			t.Fatalf("absent mismatch for %s", raw)
		}
		if !absent && o.Body.ContentLength != 0 {
			t.Fatal(o.Body.ContentLength)
		}
		if body != nil && body.Text != nil {
			encoded, _ := json.Marshal(body)
			if !strings.Contains(string(encoded), `"text":""`) {
				t.Fatal(string(encoded))
			}
		}
	}
}
