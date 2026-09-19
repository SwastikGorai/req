package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
)

// SecretHeaders returns the optional config.json header names that should be
// redacted from response metadata. Unknown config fields remain allowed for
// forward compatibility with the existing user-edited config file.
func (w *Workspace) SecretHeaders() ([]string, error) {
	data, err := os.ReadFile(filepath.Join(w.Dir(), "config.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading workspace config: %w", err)
	}
	var config struct {
		SecretHeaders []string `json:"secret_headers"`
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&config); err != nil {
		return nil, fmt.Errorf("invalid workspace config: %w", err)
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("invalid workspace config: trailing data")
	}
	for _, name := range config.SecretHeaders {
		if strings.TrimSpace(name) != name || textproto.CanonicalMIMEHeaderKey(name) == "" {
			return nil, fmt.Errorf("invalid secret header name %q", name)
		}
	}
	return config.SecretHeaders, nil
}
