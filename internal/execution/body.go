package execution

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"req/internal/model"
)

// resolveBody works on an execution copy, preserving saved file references.
func resolveBody(saved model.Request, ov Overrides, pol Policy) (*model.Body, error) {
	source := saved.Body
	if ov.Body != nil {
		source = ov.Body
	}
	if err := source.Validate(); err != nil {
		return nil, err
	}
	if source == nil {
		return nil, nil
	}
	b := *source
	resolve := pol.Variables.ResolveString
	file := func(path string, untrusted bool, location string) (string, error) {
		path, err := resolve(path, location)
		if err != nil {
			return "", err
		}
		if path == "" {
			return "", fmt.Errorf("%s: empty file path", location)
		}
		return bodyFilePath(path, pol.BodyBase, untrusted)
	}
	switch b.Type {
	case "raw", "json":
		text := ""
		if b.Text != nil {
			text = *b.Text
		}
		if b.File != "" {
			path, err := file(b.File, b.FileUntrusted, "body file")
			if err != nil {
				return nil, err
			}
			b.File, b.FileUntrusted = path, false
			if b.Type == "raw" {
				return &b, nil
			}
			info, err := os.Stat(path)
			if err != nil {
				return nil, err
			}
			if !info.Mode().IsRegular() {
				return nil, fmt.Errorf("JSON body file must be a regular file")
			}
			// ponytail: JSON files are buffered for substitution and validation;
			// use raw file streaming for large binary uploads.
			data, err := os.ReadFile(path)
			if err != nil {
				return nil, err
			}
			text, b.File = string(data), ""
		}
		text, err := resolve(text, "body")
		if err != nil {
			return nil, err
		}
		if b.Type == "json" && !json.Valid([]byte(text)) {
			return nil, fmt.Errorf("body is not valid JSON after substitution")
		}
		b.Text = &text
	case "urlencoded":
		b.URLEncoded = nil
		for i, entry := range source.URLEncoded {
			if !entry.Enabled {
				continue
			}
			var err error
			entry.Key, err = resolve(entry.Key, fmt.Sprintf("urlencoded[%d] key", i))
			if err != nil {
				return nil, err
			}
			entry.Value, err = resolve(entry.Value, fmt.Sprintf("urlencoded[%d] value", i))
			if err != nil {
				return nil, err
			}
			b.URLEncoded = append(b.URLEncoded, entry)
		}
	case "multipart":
		b.Multipart = nil
		for i, field := range source.Multipart {
			if !field.Enabled {
				continue
			}
			var err error
			location := fmt.Sprintf("multipart[%d]", i)
			for _, value := range []*string{&field.Key, &field.ContentType, &field.Filename} {
				*value, err = resolve(*value, location)
				if err != nil {
					return nil, err
				}
			}
			if field.File != "" {
				field.File, err = file(field.File, field.FileUntrusted, location+" file")
				if err != nil {
					return nil, err
				}
				field.FileUntrusted = false
			} else {
				text := ""
				if field.Value != nil {
					text = *field.Value
				}
				text, err = resolve(text, location+" value")
				if err != nil {
					return nil, err
				}
				field.Value = &text
			}
			b.Multipart = append(b.Multipart, field)
		}
	}
	return &b, b.Validate()
}

func bodyFilePath(path, base string, untrusted bool) (string, error) {
	if untrusted && (base == "" || !filepath.IsLocal(path)) {
		return "", fmt.Errorf("untrusted attachment %q: edit/remap to a workspace-relative path before use", path)
	}
	resolved := path
	if !filepath.IsAbs(path) && base != "" {
		resolved = filepath.Join(base, path)
	}
	resolved, err := filepath.Abs(resolved)
	if err != nil {
		return "", err
	}
	if untrusted {
		root, err := filepath.EvalSymlinks(base)
		if err != nil {
			return "", err
		}
		real, err := filepath.EvalSymlinks(resolved)
		if err != nil {
			return "", err
		}
		rel, err := filepath.Rel(root, real)
		if err != nil || !filepath.IsLocal(rel) {
			return "", fmt.Errorf("untrusted attachment %q escapes workspace: edit/remap it before use", path)
		}
		resolved = real
	}
	return resolved, nil
}
