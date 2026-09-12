package httpclient

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"req/internal/model"
)

// Body describes replayable bytes and files without retaining open handles.
type Body struct {
	ContentType   string
	ContentLength int64
	parts         []bodyPart
}

type bodyPart struct {
	text, path string
	size       int64
}

// BuildBody expects resolved values and paths. File contents are never copied
// into memory; only the multipart framing and text fields are buffered.
func BuildBody(def *model.Body, contentType string) (*Body, error) {
	if err := def.Validate(); err != nil {
		return nil, err
	}
	if def == nil || def.Type == "none" {
		return nil, nil
	}
	if def.FileUntrusted {
		return nil, fmt.Errorf("untrusted body path must be resolved before building HTTP")
	}
	b := &Body{ContentType: contentType}
	switch def.Type {
	case "raw", "json":
		if def.File != "" {
			if err := b.addFile(def.File); err != nil {
				return nil, err
			}
		} else if def.Text != nil {
			b.addText(*def.Text)
		} else {
			b.addText("")
		}
		if b.ContentType == "" {
			b.ContentType = "text/plain"
			if def.File != "" {
				b.ContentType = "application/octet-stream"
			}
			if def.Type == "json" {
				b.ContentType = "application/json"
			}
		}
	case "urlencoded":
		var entries []string
		for _, e := range def.URLEncoded {
			if e.Enabled {
				entries = append(entries, url.QueryEscape(e.Key)+"="+url.QueryEscape(e.Value))
			}
		}
		b.addText(strings.Join(entries, "&"))
		if b.ContentType == "" {
			b.ContentType = "application/x-www-form-urlencoded"
		}
	case "multipart":
		var framing bytes.Buffer
		writer := multipart.NewWriter(&framing)
		params := map[string]string{}
		if contentType != "" {
			media, parsed, err := mime.ParseMediaType(contentType)
			if err != nil || media != "multipart/form-data" {
				return nil, fmt.Errorf("multipart body requires Content-Type multipart/form-data")
			}
			params = parsed
			if boundary, ok := params["boundary"]; ok {
				if err := writer.SetBoundary(boundary); err != nil {
					return nil, err
				}
			}
		}
		params["boundary"] = writer.Boundary()
		b.ContentType = mime.FormatMediaType("multipart/form-data", params)
		for _, f := range def.Multipart {
			if !f.Enabled {
				continue
			}
			if f.FileUntrusted {
				return nil, fmt.Errorf("untrusted multipart path must be resolved before building HTTP")
			}
			filename := f.Filename
			if f.File != "" && filename == "" {
				filename = filepath.Base(f.File)
			}
			for _, v := range []string{f.Key, filename, f.ContentType} {
				if strings.IndexFunc(v, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0 {
					return nil, fmt.Errorf("multipart metadata contains a control character")
				}
			}
			disposition := map[string]string{"name": f.Key}
			if f.File != "" {
				disposition["filename"] = filename
			}
			h := textproto.MIMEHeader{"Content-Disposition": {mime.FormatMediaType("form-data", disposition)}}
			partType := f.ContentType
			if partType == "" && f.File != "" {
				partType = "application/octet-stream"
			}
			if partType != "" {
				if _, _, err := mime.ParseMediaType(partType); err != nil {
					return nil, fmt.Errorf("invalid multipart content type: %w", err)
				}
				h.Set("Content-Type", partType)
			}
			if _, err := writer.CreatePart(h); err != nil {
				return nil, err
			}
			b.addText(framing.String())
			framing.Reset()
			if f.File != "" {
				if err := b.addFile(f.File); err != nil {
					return nil, err
				}
			} else if f.Value != nil {
				b.addText(*f.Value)
			}
		}
		if err := writer.Close(); err != nil {
			return nil, err
		}
		b.addText(framing.String())
	}
	return b, nil
}

func (b *Body) addText(text string) {
	b.parts = append(b.parts, bodyPart{text: text, size: int64(len(text))})
	b.ContentLength += int64(len(text))
}

func (b *Body) addFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("body file %q: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("body file %q must be a regular file", path)
	}
	b.parts = append(b.parts, bodyPart{path: path, size: info.Size()})
	b.ContentLength += info.Size()
	return nil
}

// Open creates an independent reader for the first send or a 307/308 replay.
// Opening all attachments up front makes unreadable-file failures precede HTTP.
// ponytail: holds one descriptor per attachment; open lazily if huge part counts are needed.
func (b *Body) Open(ctx context.Context) (io.ReadCloser, error) {
	if b == nil {
		return nil, nil
	}
	r := &bodyReader{source: b, ctx: ctx}
	var readers []io.Reader
	for _, part := range b.parts {
		if err := ctx.Err(); err != nil {
			r.Close()
			return nil, err
		}
		if part.path == "" {
			readers = append(readers, strings.NewReader(part.text))
			continue
		}
		f, err := os.Open(part.path)
		if err != nil {
			r.Close()
			return nil, err
		}
		r.files = append(r.files, f)
		info, err := f.Stat()
		if err != nil {
			r.Close()
			return nil, err
		}
		if !info.Mode().IsRegular() || info.Size() != part.size {
			r.Close()
			return nil, fmt.Errorf("body file %q changed after preparation", part.path)
		}
		readers = append(readers, io.LimitReader(f, part.size))
	}
	r.reader = io.MultiReader(readers...)
	return r, nil
}

type bodyReader struct {
	reader   io.Reader
	source   *Body
	ctx      context.Context
	files    []*os.File
	once     sync.Once
	closeErr error
}

func (r *bodyReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

func (r *bodyReader) Close() error {
	r.once.Do(func() {
		for _, f := range r.files {
			r.closeErr = errors.Join(r.closeErr, f.Close())
		}
	})
	return r.closeErr
}
