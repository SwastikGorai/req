package output

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"req/internal/scripting"
)

func TestJSONSingleEnvelope(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := Render(Result{
		StatusCode: 200,
		StatusText: "OK",
		Headers:    [][2]string{{"Authorization", "secret"}, {"X-Trace", "trace"}},
		Duration:   12 * time.Millisecond,
		Body:       []byte(`{"ok":true}`),
		HasBody:    true,
		Tests:      []scripting.TestResult{{Name: "ok", Failed: false}},
		Logs:       []string{"hello"},
	}, Options{Format: "json", Verbose: true, SecretHeaders: []string{"X-Trace"}}, &stdout, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	var envelope map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("envelope is not JSON: %v (%q)", err, stdout.String())
	}
	if envelope["version"] != float64(1) || envelope["status_code"] != float64(200) {
		t.Fatalf("envelope metadata = %#v", envelope)
	}
	dec := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
	var first, extra any
	if err := dec.Decode(&first); err != nil {
		t.Fatal(err)
	}
	if err := dec.Decode(&extra); err != io.EOF {
		t.Fatalf("stdout has more than one JSON value: %v", err)
	}
	if !strings.Contains(stderr.String(), "Authorization: [REDACTED]") || !strings.Contains(stderr.String(), "X-Trace: [REDACTED]") {
		t.Fatalf("stderr = %q, want redacted headers", stderr.String())
	}
	if strings.Contains(stdout.String(), "secret") || strings.Contains(stdout.String(), "trace") {
		t.Fatalf("JSON metadata leaked a header value: %s", stdout.String())
	}
}

func TestBinaryBase64Envelope(t *testing.T) {
	var stdout bytes.Buffer
	body := []byte{0, 1, 2, 255}
	if err := Render(Result{StatusCode: 200, Body: body, HasBody: true}, Options{Format: "json"}, &stdout, io.Discard); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Body     string `json:"body"`
		Encoding string `json:"body_encoding"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Encoding != "base64" || got.Body != base64.StdEncoding.EncodeToString(body) {
		t.Fatalf("body = %#v, want base64", got)
	}
}

func TestTerminalPrettyJSON(t *testing.T) {
	result := Result{StatusCode: 200, ContentType: "application/json", Body: []byte(`{"a":1}`), HasBody: true}
	var terminal, pipe bytes.Buffer
	if err := Render(result, Options{Terminal: true}, &terminal, io.Discard); err != nil {
		t.Fatal(err)
	}
	if err := Render(result, Options{}, &pipe, io.Discard); err != nil {
		t.Fatal(err)
	}
	if terminal.String() != "{\n  \"a\": 1\n}\n" {
		t.Errorf("terminal output = %q", terminal.String())
	}
	if pipe.String() != `{"a":1}` {
		t.Errorf("pipeline output = %q", pipe.String())
	}
}

func TestOutputPathReference(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/response.bin"
	if err := WriteFileAtomic(context.Background(), path, bytes.NewReader([]byte("download"))); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	if err := Render(Result{StatusCode: 200, BodyPath: path}, Options{Format: "json"}, &stdout, io.Discard); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Body     string `json:"body"`
		BodyPath string `json:"body_path"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Body != "" || got.BodyPath != path {
		t.Fatalf("output reference = %#v", got)
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("body failed") }

func TestOutputPathFailureLeavesDestination(t *testing.T) {
	path := t.TempDir() + "/response.bin"
	if err := WriteFileAtomic(context.Background(), path, bytes.NewReader([]byte("old"))); err != nil {
		t.Fatal(err)
	}
	if err := WriteFileAtomic(context.Background(), path, failingReader{}); err == nil {
		t.Fatal("expected body failure")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "old" {
		t.Fatalf("destination = %q, err=%v; want original bytes", data, err)
	}
}
