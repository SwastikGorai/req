package importer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"req/internal/store"
)

func fixtureBytes(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestPostmanNestedData(t *testing.T) {
	result, err := ParsePostmanCollection(fixtureBytes(t, "synthetic-collection.json"), Options{Source: "collection.json"})
	if err != nil {
		t.Fatalf("ParsePostmanCollection: %v", err)
	}
	c := result.Collection
	if c.ID != "pm-col-001" || c.Name != "Imported API" {
		t.Fatalf("collection identity = %q/%q", c.ID, c.Name)
	}
	if c.Variables["old_token"] != "disabled" || !c.DisabledVariables["old_token"] {
		t.Fatalf("disabled variable was not retained: %#v/%#v", c.Variables, c.DisabledVariables)
	}
	if len(c.Items) != 2 || c.Items[0].Type != "folder" || len(c.Items[0].Folder.Children) != 2 {
		t.Fatalf("nested items were not retained: %+v", c.Items)
	}
	list := c.Items[0].Folder.Children[0]
	if list.Request.URL != "{{base}}/users?active=true" || len(list.Request.Query) != 1 || list.Request.Query[0].Key != "page" || list.Request.Query[0].Enabled {
		t.Fatalf("URL/query normalization = %#v/%#v", list.Request.URL, list.Request.Query)
	}
	if list.Request.Auth == nil || list.Request.Auth.Type != "basic" || list.Request.Auth.Username != "{{user}}" {
		t.Fatalf("request auth = %+v", list.Request.Auth)
	}
	if !c.Items[0].Folder.Children[1].Disabled {
		t.Error("disabled request flag was not retained")
	}
	upload := c.Items[1].Request
	if upload.Body == nil || upload.Body.Type != "multipart" || len(upload.Body.Multipart) != 2 || !upload.Body.Multipart[1].FileUntrusted {
		t.Fatalf("multipart body = %+v", upload.Body)
	}
	if len(c.Scripts.PreRequest) != 1 || c.Scripts.PreRequest[0].Source != "pm.variables.set('ready', 'yes');" || len(c.Scripts.PostResponse) != 1 {
		t.Fatalf("scripts were not normalized: %+v", c.Scripts)
	}
	if result.Counts.Folders != 1 || result.Counts.Requests != 3 || result.Counts.Scripts != 3 {
		t.Fatalf("counts = %+v", result.Counts)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("clean fixture warnings = %+v", result.Warnings)
	}
}

func TestPostmanStrictNoWrite(t *testing.T) {
	ws, _, err := store.Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	data := []byte(`{"info":{"_postman_id":"strict-1","name":"Strict","schema":"https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},"item":[{"name":"R","request":{"method":"GET","url":"https://example.test","auth":{"type":"oauth2"}}}]}`)
	_, err = ImportPostmanCollection(context.Background(), ws, data, Options{Source: "lossy.json", Strict: true})
	var strict *StrictError
	if !errors.As(err, &strict) {
		t.Fatalf("strict import error = %v, want StrictError", err)
	}
	if got, listErr := ws.ListCollections(context.Background()); listErr != nil || len(got) != 0 {
		t.Fatalf("strict import wrote data: %d collections, %v", len(got), listErr)
	}
}

func TestPostmanMalformedNoWrite(t *testing.T) {
	ws, _, err := store.Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, err = ImportPostmanCollection(context.Background(), ws, []byte(`{"info":`), Options{})
	if err == nil || !strings.Contains(err.Error(), "postman collection") {
		t.Fatalf("malformed import error = %v", err)
	}
	if got, listErr := ws.ListCollections(context.Background()); listErr != nil || len(got) != 0 {
		t.Fatalf("malformed import wrote data: %d collections, %v", len(got), listErr)
	}
}

func TestPostmanCollisionHandling(t *testing.T) {
	ws, _, err := store.Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	data := fixtureBytes(t, "synthetic-collection.json")
	if _, err := ImportPostmanCollection(context.Background(), ws, data, Options{Source: "one.json"}); err != nil {
		t.Fatal(err)
	}
	result, err := ImportPostmanCollection(context.Background(), ws, data, Options{Source: "two.json"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Collection.Name != "Imported API (2)" || len(result.Warnings) != 1 || result.Warnings[0].Code != "name-collision" {
		t.Fatalf("collision result = %q/%+v", result.Collection.Name, result.Warnings)
	}
	_, err = ImportPostmanCollection(context.Background(), ws, data, Options{Name: "Imported API"})
	if !errors.Is(err, store.ErrDuplicateName) {
		t.Fatalf("explicit collision = %v, want duplicate name", err)
	}
}

func TestPostmanUnsupportedAuthBlocked(t *testing.T) {
	result, err := ParsePostmanCollection([]byte(`{"info":{"_postman_id":"auth-1","name":"Auth","schema":"https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},"item":[{"name":"R","request":{"method":"GET","url":"https://example.test","auth":{"type":"oauth2"}}}]}`), Options{})
	if err != nil {
		t.Fatal(err)
	}
	request := result.Collection.Items[0].Request
	if request.Import == nil || !request.Import.Blocked() || request.Auth == nil || request.Auth.Type != "none" {
		t.Fatalf("unsupported auth was not blocked: %+v", request)
	}
}

func TestPostmanMalformedAuthBlocked(t *testing.T) {
	data := []byte(`{"info":{"name":"Auth","schema":"https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},"item":[{"name":"R","request":{"method":"GET","url":"https://example.test","auth":{"type":"bearer","bearer":[{"key":"token"}]}}}]}`)
	result, err := ParsePostmanCollection(data, Options{})
	if err != nil {
		t.Fatal(err)
	}
	request := result.Collection.Items[0].Request
	if request.Import == nil || !request.Import.Blocked() || len(result.Warnings) == 0 {
		t.Fatalf("malformed auth was not blocked: request=%+v warnings=%+v", request, result.Warnings)
	}
}

func TestPostmanUnsupportedInheritedAuthCanBeOverridden(t *testing.T) {
	data := []byte(`{"info":{"name":"Auth","schema":"https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},"auth":{"type":"oauth2"},"item":[{"name":"F","item":[{"name":"R","request":{"method":"GET","url":"https://example.test","auth":{"type":"noauth"}}}]}]}`)
	result, err := ParsePostmanCollection(data, Options{})
	if err != nil {
		t.Fatal(err)
	}
	request := result.Collection.Items[0].Folder.Children[0].Request
	if request.Import != nil && request.Import.Blocked() {
		t.Fatalf("explicit noauth inherited unsupported auth: %+v", request.Import)
	}
}

func TestPostmanEnvironmentImport(t *testing.T) {
	result, err := ParsePostmanEnvironment(fixtureBytes(t, "synthetic-environment.json"), Options{Source: "environment.json"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Environment.Name != "Local" || result.Environment.Variables["secret"] != "hidden" || !result.Environment.DisabledVariables["secret"] {
		t.Fatalf("environment = %+v", result.Environment)
	}
	if got := result.Environment.ActiveVariables(); len(got) != 1 || got["base"] == nil {
		t.Fatalf("active variables = %#v", got)
	}
	ws, _, err := store.Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ImportPostmanEnvironment(context.Background(), ws, fixtureBytes(t, "synthetic-environment.json"), Options{Source: "environment.json"}); err != nil {
		t.Fatal(err)
	}
	_, err = ImportPostmanEnvironment(context.Background(), ws, fixtureBytes(t, "synthetic-environment.json"), Options{Name: "Local"})
	if !errors.Is(err, store.ErrDuplicateName) {
		t.Fatalf("explicit environment collision = %v", err)
	}
}

func TestPostmanStrictUnsupportedSchema(t *testing.T) {
	_, err := ParsePostmanCollection([]byte(`{"info":{"name":"Old","schema":"https://schema.getpostman.com/json/collection/v2.0.0/collection.json"},"item":[]}`), Options{Strict: true})
	if err == nil || !strings.Contains(err.Error(), "unsupported schema") {
		t.Fatalf("schema error = %v", err)
	}
}

func TestPostmanStructuredPathVariableWithRawQuery(t *testing.T) {
	data := []byte(`{"info":{"_postman_id":"url-1","name":"URL","schema":"https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},"item":[{"name":"R","request":{"method":"GET","url":{"raw":"https://api.example.test/users/:id?x=1","protocol":"https","host":["api","example","test"],"path":["users",":id"],"query":[{"key":"x","value":"1"}],"variable":[{"key":"id","value":"42"}]}}}]}`)
	result, err := ParsePostmanCollection(data, Options{})
	if err != nil {
		t.Fatal(err)
	}
	request := result.Collection.Items[0].Request
	if request.URL != "https://api.example.test/users/42?x=1" || len(request.Query) != 0 {
		t.Fatalf("normalized URL/query = %q/%+v", request.URL, request.Query)
	}
}

func TestPostmanFileBodyIsUntrusted(t *testing.T) {
	data := []byte(`{"info":{"name":"File","schema":"https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},"item":[{"name":"Upload","request":{"method":"POST","url":"https://example.test","body":{"mode":"file","file":{"src":"payload.bin"}}}}]}`)
	result, err := ParsePostmanCollection(data, Options{})
	if err != nil {
		t.Fatal(err)
	}
	body := result.Collection.Items[0].Request.Body
	if body == nil || body.Type != "raw" || body.File != "payload.bin" || !body.FileUntrusted || len(result.Warnings) != 0 {
		t.Fatalf("file body = %+v warnings=%+v", body, result.Warnings)
	}
}

func TestPostmanLenientNamesAndIDsAreDeterministic(t *testing.T) {
	data := []byte(`{"info":{"_postman_id":"bad/id","name":"A/B","schema":"https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},"item":[{"id":"same/id","name":"X/Y","request":{"method":"GET","url":"https://example.test"}},{"id":"same/id","name":"X/Y","request":{"method":"GET","url":"https://example.test"}}]}`)
	first, err := ParsePostmanCollection(data, Options{Source: "names.json"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := ParsePostmanCollection(data, Options{Source: "names.json"})
	if err != nil {
		t.Fatal(err)
	}
	if first.Collection.Name != "A_B" || first.Collection.Items[0].Name != "X_Y" || first.Collection.Items[1].Name != "X_Y (2)" {
		t.Fatalf("normalized names = %q/%q/%q", first.Collection.Name, first.Collection.Items[0].Name, first.Collection.Items[1].Name)
	}
	if first.Collection.ID != second.Collection.ID || first.Collection.Items[0].ID != second.Collection.Items[0].ID || first.Collection.Items[1].ID != second.Collection.Items[1].ID || len(first.Warnings) < 4 {
		t.Fatalf("normalization is not deterministic: %#v / %#v", first.Collection, first.Warnings)
	}
	if _, err := ParsePostmanCollection(data, Options{Source: "names.json", Strict: true}); !errors.As(err, new(*StrictError)) {
		t.Fatalf("strict normalized names error = %v", err)
	}
}
