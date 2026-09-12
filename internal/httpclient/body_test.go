package httpclient

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"os"
	"path/filepath"
	"testing"

	"req/internal/model"
)

func TestMultipartBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "source.bin")
	want := []byte{0, 255, '\r', '\n', '{', '{', 'x', '}', '}'}
	if err := os.WriteFile(path, want, 0600); err != nil {
		t.Fatal(err)
	}
	text := "literal @value"
	b, err := BuildBody(&model.Body{Type: "multipart", Multipart: []model.MultipartField{
		{Key: "label", Value: &text, Enabled: true},
		{Key: "upload", File: path, Filename: "override.bin", ContentType: "application/x-test", Enabled: true},
		{Key: "disabled", File: "missing.bin"},
	}}, "multipart/form-data; boundary=chosen")
	if err != nil {
		t.Fatal(err)
	}
	r, err := b.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	encoded, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if int64(len(encoded)) != b.ContentLength {
		t.Fatalf("length %d != %d", len(encoded), b.ContentLength)
	}
	_, params, err := mime.ParseMediaType(b.ContentType)
	if err != nil {
		t.Fatal(err)
	}
	if params["boundary"] != "chosen" {
		t.Fatal(params)
	}
	mr := multipart.NewReader(bytes.NewReader(encoded), params["boundary"])
	p, err := mr.NextPart()
	if err != nil {
		t.Fatal(err)
	}
	value, _ := io.ReadAll(p)
	if p.FormName() != "label" || string(value) != text {
		t.Fatalf("%v %q", p.Header, value)
	}
	p, err = mr.NextPart()
	if err != nil {
		t.Fatal(err)
	}
	value, _ = io.ReadAll(p)
	if p.FileName() != "override.bin" || p.Header.Get("Content-Type") != "application/x-test" || !bytes.Equal(value, want) {
		t.Fatalf("%v %v", p.Header, value)
	}
	if _, err := mr.NextPart(); err != io.EOF {
		t.Fatal(err)
	}
}

func TestBodyStreamCleanup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "large.bin")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(64 << 20); err != nil {
		t.Fatal(err)
	}
	f.Close()
	b, err := BuildBody(&model.Body{Type: "multipart", Multipart: []model.MultipartField{{Key: "file", File: path, Enabled: true}}}, "")
	if err != nil {
		t.Fatal(err)
	}
	buffered := 0
	for _, part := range b.parts {
		buffered += len(part.text)
	}
	if buffered > 1024 {
		t.Fatalf("file was buffered: %d bytes", buffered)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rc, err := b.Open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	r := rc.(*bodyReader)
	defer r.Close()
	if _, err := io.ReadFull(r, make([]byte, 4096)); err != nil {
		t.Fatal(err)
	}
	cancel()
	if _, err := r.Read(make([]byte, 1)); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	for _, file := range r.files {
		if _, err := file.Stat(); err == nil {
			t.Fatalf("file still open: %v", err)
		}
	}
	if _, err := b.Open(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled open: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Open(context.Background()); err == nil {
		t.Fatal("missing file reopened")
	}
}

func TestBodyMetadataValidation(t *testing.T) {
	for _, ct := range []string{"application/json", "multipart/form-data; boundary=", "multipart/form-data; boundary=\"bad\r\nvalue\""} {
		if _, err := BuildBody(&model.Body{Type: "multipart"}, ct); err == nil {
			t.Fatalf("accepted %q", ct)
		}
	}
	for _, field := range []model.MultipartField{
		{Key: "bad\x01name", Enabled: true},
		{Key: "bad\r\nname", Enabled: true},
		{Key: "ok", ContentType: "bad type", Enabled: true},
		{Key: "file", File: "outside", FileUntrusted: true, Enabled: true},
	} {
		if _, err := BuildBody(&model.Body{Type: "multipart", Multipart: []model.MultipartField{field}}, ""); err == nil {
			t.Fatal("invalid multipart metadata accepted")
		}
	}
}
