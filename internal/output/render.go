package output

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/SwastikGorai/req/internal/scripting"
)

const envelopeVersion = 1

// Options controls response rendering for send and run.
type Options struct {
	Format        string
	OutputPath    string
	Raw           bool
	Verbose       bool
	Terminal      bool
	SecretHeaders []string
}

func (o Options) JSON() bool { return o.Format == "json" }

// Validate checks the small output flag surface shared by send and run.
func (o Options) Validate() error {
	if o.Format != "" && o.Format != "json" {
		return fmt.Errorf("unsupported output format %q (want json)", o.Format)
	}
	if o.Raw && o.JSON() {
		return errors.New("--raw cannot be combined with --output-format json")
	}
	return nil
}

// Result is the aggregated response and script state for one execution.
type Result struct {
	StatusCode  int
	StatusText  string
	Headers     [][2]string
	Duration    time.Duration
	ContentType string
	Body        []byte
	HasBody     bool
	BodyPath    string
	Tests       []scripting.TestResult
	Logs        []string
	Errors      []string
	Skipped     bool
}

type envelope struct {
	Version      int                    `json:"version"`
	StatusCode   int                    `json:"status_code"`
	StatusText   string                 `json:"status_text"`
	Headers      []header               `json:"headers"`
	DurationMS   int64                  `json:"duration_ms"`
	Body         any                    `json:"body,omitempty"`
	BodyEncoding string                 `json:"body_encoding,omitempty"`
	BodyPath     string                 `json:"body_path,omitempty"`
	Tests        []scripting.TestResult `json:"tests"`
	Logs         []string               `json:"logs"`
	Errors       []string               `json:"errors"`
	Skipped      bool                   `json:"skipped"`
}

type header struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// Render writes diagnostics to stderr and either one JSON envelope or the
// response body to stdout. Body content and script logs are deliberately not
// redacted; only metadata headers pass through redactHeader.
func Render(result Result, opts Options, stdout, stderr io.Writer) error {
	if opts.Verbose {
		if err := renderHeaders(result.Headers, opts, stderr); err != nil {
			return err
		}
	}
	if opts.JSON() {
		return json.NewEncoder(stdout).Encode(makeEnvelope(result, opts))
	}
	if result.BodyPath != "" || !result.HasBody {
		return nil
	}
	body := result.Body
	if opts.Terminal && !opts.Raw && (jsonContent(result.ContentType) || json.Valid(body)) {
		var pretty bytes.Buffer
		if err := json.Indent(&pretty, body, "", "  "); err == nil {
			pretty.WriteByte('\n')
			body = pretty.Bytes()
		}
	}
	_, err := stdout.Write(body)
	return err
}

func makeEnvelope(result Result, opts Options) envelope {
	headers := make([]header, 0, len(result.Headers))
	for _, entry := range result.Headers {
		headers = append(headers, header{Name: entry[0], Value: redactHeader(entry[0], entry[1], opts.SecretHeaders)})
	}
	tests := result.Tests
	if tests == nil {
		tests = []scripting.TestResult{}
	}
	logs := result.Logs
	if logs == nil {
		logs = []string{}
	}
	errors := result.Errors
	if errors == nil {
		errors = []string{}
	}
	out := envelope{
		Version:    envelopeVersion,
		StatusCode: result.StatusCode,
		StatusText: result.StatusText,
		Headers:    headers,
		DurationMS: result.Duration.Milliseconds(),
		Tests:      tests,
		Logs:       logs,
		Errors:     errors,
		Skipped:    result.Skipped,
	}
	if result.BodyPath != "" {
		out.BodyPath = result.BodyPath
	} else if result.HasBody {
		if isText(result.Body) {
			out.Body = string(result.Body)
			out.BodyEncoding = "text"
		} else {
			out.Body = base64.StdEncoding.EncodeToString(result.Body)
			out.BodyEncoding = "base64"
		}
	}
	return out
}

func renderHeaders(entries [][2]string, opts Options, stderr io.Writer) error {
	for _, entry := range entries {
		if _, err := fmt.Fprintf(stderr, "%s: %s\n", entry[0], redactHeader(entry[0], entry[1], opts.SecretHeaders)); err != nil {
			return err
		}
	}
	return nil
}

func redactHeader(name, value string, configured []string) string {
	for _, sensitive := range []string{"Authorization", "Proxy-Authorization", "Cookie", "Set-Cookie"} {
		if strings.EqualFold(name, sensitive) {
			return "[REDACTED]"
		}
	}
	for _, sensitive := range configured {
		if strings.EqualFold(name, sensitive) {
			return "[REDACTED]"
		}
	}
	return value
}

func jsonContent(contentType string) bool {
	if contentType == "" {
		return false
	}
	media, _, err := mime.ParseMediaType(contentType)
	return err == nil && (media == "application/json" || strings.HasSuffix(media, "+json"))
}

func isText(body []byte) bool {
	return utf8.Valid(body) && !bytes.Contains(body, []byte{0})
}

// WriteFileAtomic streams body into a same-directory temporary file and only
// replaces path after the complete copy is synced and closed. Filesystem
// failures are wrapped so execution can distinguish them from body reads.
func WriteFileAtomic(ctx context.Context, path string, body io.Reader) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-")
	if err != nil {
		return &FileError{Op: "create output", Err: err}
	}
	tmpName := tmp.Name()
	defer func() {
		if tmpName != "" {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := io.Copy(tmp, &contextReader{ctx: ctx, reader: body}); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := ctx.Err(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return &FileError{Op: "sync output", Err: err}
	}
	if err := tmp.Close(); err != nil {
		return &FileError{Op: "close output", Err: err}
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return &FileError{Op: "set output permissions", Err: err}
	}
	if err := os.Rename(tmpName, path); err != nil {
		return &FileError{Op: "replace output", Err: err}
	}
	tmpName = ""
	return nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

// FileError marks failures in the output destination rather than the response
// body stream.
type FileError struct {
	Op  string
	Err error
}

func (e *FileError) Error() string { return fmt.Sprintf("%s: %v", e.Op, e.Err) }
func (e *FileError) Unwrap() error { return e.Err }
