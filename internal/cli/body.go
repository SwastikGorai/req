package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"req/internal/model"
)

// parseBodyFlag is shared by send, run and request create. It stores file
// references, never file contents; only execution opens attachments.
func parseBodyFlag(body **model.Body, flag, value string) error {
	kind := "raw"
	switch flag {
	case "--json":
		kind = "json"
	case "--urlencoded":
		kind = "urlencoded"
	case "--form", "--form-file":
		kind = "multipart"
	}
	if *body != nil && ((*body).Type != kind || (kind != "multipart" && kind != "urlencoded")) {
		return fmt.Errorf("%s conflicts with another body flag", flag)
	}
	if *body == nil {
		*body = &model.Body{Type: kind}
	}
	b := *body
	switch flag {
	case "--body":
		b.Text = &value
	case "--body-file":
		if value == "" {
			return fmt.Errorf("--body-file requires a nonempty path")
		}
		b.File = value
	case "--json":
		if path, file := strings.CutPrefix(value, "@"); file {
			if path == "" {
				return fmt.Errorf("--json @ requires a file path")
			}
			b.File = path
		} else {
			if !strings.Contains(value, "{{") && !json.Valid([]byte(value)) {
				return fmt.Errorf("--json value is not valid JSON")
			}
			b.Text = &value
		}
	case "--urlencoded", "--form", "--form-file":
		key, val, ok := strings.Cut(value, "=")
		if !ok || key == "" {
			return fmt.Errorf("%s requires KEY=VALUE", flag)
		}
		if flag == "--urlencoded" {
			b.URLEncoded = append(b.URLEncoded, model.Entry{Key: key, Value: val, Enabled: true})
		} else {
			field := model.MultipartField{Key: key, Enabled: true}
			if flag == "--form-file" {
				if val == "" {
					return fmt.Errorf("--form-file requires KEY=PATH with a nonempty path")
				}
				field.File = val
			} else {
				field.Value = &val
			}
			b.Multipart = append(b.Multipart, field)
		}
	default:
		return fmt.Errorf("unknown body flag %s", flag)
	}
	return nil
}
