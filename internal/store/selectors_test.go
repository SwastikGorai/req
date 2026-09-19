package store

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/SwastikGorai/req/internal/model"
)

func TestSplitPath(t *testing.T) {
	valid := []struct {
		path string
		want []string
	}{
		{path: "API/Auth/Login", want: []string{"API", "Auth", "Login"}},
		{path: "API/Ping", want: []string{"API", "Ping"}},
		{path: "API", want: []string{"API"}},
		// Only exact "."/".." are special; "B.." is a plain name.
		{path: "A/B..", want: []string{"A", "B.."}},
		{path: "A/...", want: []string{"A", "..."}},
	}
	for _, tc := range valid {
		got, err := SplitPath(tc.path)
		if err != nil {
			t.Errorf("SplitPath(%q): %v, want valid", tc.path, err)
			continue
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("SplitPath(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}

	invalid := []string{
		"",
		"A//B", // empty segment
		"/A",
		"A/",
		"A/./B",
		"A/../B",
		`A\.B`, // backslash is a path separator on Windows
		"A/.",
		"A/..",
	}
	for _, path := range invalid {
		if _, err := SplitPath(path); !errors.Is(err, ErrInvalidPath) {
			t.Errorf("SplitPath(%q) error = %v, want ErrInvalidPath", path, err)
		}
	}
}

func TestResolvePath(t *testing.T) {
	ws := newTestWorkspace(t)
	ctx := context.Background()
	if _, err := ws.CreateCollection(ctx, "API"); err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	if _, err := ws.CreateFolder(ctx, "API/Auth", true); err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	if err := ws.CreateRequest(ctx, "API/Auth/Login", model.Request{Method: "POST", URL: "{{base_url}}/login"}, false); err != nil {
		t.Fatalf("CreateRequest(Login): %v", err)
	}
	if err := ws.CreateRequest(ctx, "API/Ping", model.Request{Method: "GET", URL: "{{base_url}}/ping"}, false); err != nil {
		t.Fatalf("CreateRequest(Ping): %v", err)
	}

	// The collection root resolves to no item.
	rp, err := ws.ResolvePath(ctx, "API")
	if err != nil {
		t.Fatalf("ResolvePath(API): %v", err)
	}
	if rp.Item != nil {
		t.Errorf("ResolvePath(API).Item = %+v, want nil at the collection root", rp.Item)
	}
	if rp.Collection.Name != "API" || rp.Rev == "" {
		t.Errorf("ResolvePath(API): collection %q rev %q, want the API collection with a revision", rp.Collection.Name, rp.Rev)
	}
	if want := []string{"API"}; !reflect.DeepEqual(rp.Segments, want) {
		t.Errorf("Segments = %v, want %v", rp.Segments, want)
	}

	// A folder path resolves to the folder item.
	rp, err = ws.ResolvePath(ctx, "API/Auth")
	if err != nil {
		t.Fatalf("ResolvePath(API/Auth): %v", err)
	}
	if rp.Item == nil || rp.Item.Type != "folder" || rp.Item.Name != "Auth" || rp.Item.Folder == nil {
		t.Errorf("ResolvePath(API/Auth).Item = %+v, want the Auth folder", rp.Item)
	}

	// A request path resolves to the request item.
	rp, err = ws.ResolvePath(ctx, "API/Auth/Login")
	if err != nil {
		t.Fatalf("ResolvePath(API/Auth/Login): %v", err)
	}
	if rp.Item == nil || rp.Item.Type != "request" || rp.Item.Request == nil || rp.Item.Request.Method != "POST" {
		t.Errorf("ResolvePath(API/Auth/Login).Item = %+v, want the Login request", rp.Item)
	}

	// Missing collection and missing segment both wrap ErrNotFound.
	if _, err := ws.ResolvePath(ctx, "Missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("ResolvePath(Missing) error = %v, want ErrNotFound", err)
	}
	if _, err := ws.ResolvePath(ctx, "API/Auth/Nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("ResolvePath(API/Auth/Nope) error = %v, want ErrNotFound", err)
	}

	// A path continuing through a request fails, but not as ErrNotFound.
	_, err = ws.ResolvePath(ctx, "API/Ping/Deeper")
	if err == nil || errors.Is(err, ErrNotFound) {
		t.Errorf("ResolvePath(API/Ping/Deeper) error = %v, want a non-ErrNotFound request-has-no-children error", err)
	}

	// Malformed paths wrap ErrInvalidPath.
	if _, err := ws.ResolvePath(ctx, "API//x"); !errors.Is(err, ErrInvalidPath) {
		t.Errorf("ResolvePath(API//x) error = %v, want ErrInvalidPath", err)
	}
}
