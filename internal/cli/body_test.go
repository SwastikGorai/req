package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestBodyPathBase(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	mustRun(t, 0, "init")
	mustRun(t, 0, "collection", "create", "API")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.Copy(w, r.Body) }))
	defer srv.Close()
	// Saving references must not require the file to exist yet.
	mustRun(t, 0, "request", "create", "API/File", "--method", "POST", "--url", srv.URL, "--body-file", "payload.bin")
	mustRun(t, 0, "request", "create", "API/JSON", "--method", "POST", "--url", srv.URL, "--json", "@payload.json")
	want := []byte{0, 255, '\r', '\n', '{', '{', 'x', '}', '}'}
	if err := os.WriteFile("payload.bin", want, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("payload.json", []byte(`{"n":{{n}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	before := collectionFileBytes(t)
	nested := filepath.Join(root, "nested")
	if err := os.Mkdir(nested, 0700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(nested)
	if err := os.WriteFile("payload.bin", []byte("cwd"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("payload.json", []byte(`{"source":"cwd"}`), 0600); err != nil {
		t.Fatal(err)
	}
	got, _ := mustRun(t, 0, "run", "API/File")
	if !bytes.Equal([]byte(got), want) {
		t.Fatalf("raw bytes changed: %q", got)
	}
	got, _ = mustRun(t, 0, "run", "API/JSON", "--var", "n=7")
	if got != `{"n":7}` {
		t.Fatal(got)
	}
	got, _ = mustRun(t, 0, "run", "API/JSON", "--body-file", "payload.bin")
	if !bytes.Equal([]byte(got), want) {
		t.Fatal(got)
	}
	got, _ = mustRun(t, 0, "send", "POST", srv.URL, "--body-file", "payload.bin")
	if got != "cwd" {
		t.Fatal(got)
	}
	got, _ = mustRun(t, 0, "send", "POST", srv.URL, "--json", "@payload.json")
	if got != `{"source":"cwd"}` {
		t.Fatal(got)
	}
	mustRun(t, 2, "run", "API/JSON", "--var", "n=invalid")
	t.Chdir(root)
	if !bytes.Equal(before, collectionFileBytes(t)) {
		t.Fatal("execution rewrote saved file references")
	}
}

func TestCLIForms(t *testing.T) {
	t.Chdir(t.TempDir())
	mustRun(t, 0, "init")
	mustRun(t, 0, "collection", "create", "API")
	if err := os.WriteFile("file.bin", []byte{0, 255}, 0600); err != nil {
		t.Fatal(err)
	}
	received := make(chan []string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.Header.Get("Content-Type"), "application/x-www-form-urlencoded") {
			data, _ := io.ReadAll(r.Body)
			received <- []string{string(data)}
			return
		}
		reader, err := r.MultipartReader()
		if err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		var parts []string
		for {
			part, err := reader.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Error(err)
				break
			}
			data, err := io.ReadAll(part)
			if err != nil {
				t.Error(err)
			}
			parts = append(parts, part.FormName()+"|"+part.FileName()+"|"+string(data))
		}
		if r.ContentLength <= 0 {
			t.Errorf("missing content length: %d", r.ContentLength)
		}
		received <- parts
	}))
	defer srv.Close()
	mustRun(t, 0, "send", "POST", srv.URL, "--urlencoded", "q={{v}}", "--urlencoded", "q=", "--var", "v=a b&c")
	if got := <-received; !reflect.DeepEqual(got, []string{"q=a+b%26c&q="}) {
		t.Fatal(got)
	}
	flags := []string{"--form", "text=@literal", "--form-file", "upload=file.bin", "--form", "text="}
	for _, contentType := range []string{"multipart/form-data", "multipart/form-data; boundary=explicit"} {
		args := append([]string{"send", "POST", srv.URL, "-H", "Content-Type: " + contentType}, flags...)
		mustRun(t, 0, args...)
		if got := <-received; !reflect.DeepEqual(got, []string{"text||@literal", "upload|file.bin|" + string([]byte{0, 255}), "text||"}) {
			t.Fatal(got)
		}
	}
	mustRun(t, 0, append([]string{"request", "create", "API/Form", "--method", "POST", "--url", srv.URL}, flags...)...)
	mustRun(t, 0, "run", "API/Form")
	<-received
	mustRun(t, 0, "run", "API/Form", "--urlencoded", "saved=override")
	if got := <-received; !reflect.DeepEqual(got, []string{"saved=override"}) {
		t.Fatal(got)
	}
}

func TestBodyModeConflict(t *testing.T) {
	t.Chdir(t.TempDir())
	mustRun(t, 0, "init")
	mustRun(t, 0, "collection", "create", "API")
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer srv.Close()
	mustRun(t, 0, "request", "create", "API/R", "--method", "POST", "--url", srv.URL)
	modes := [][]string{{"--body", ""}, {"--body-file", "file"}, {"--json", "{}"}, {"--urlencoded", "q=v"}, {"--form", "q=v"}, {"--form-file", "q=file"}}
	for i, a := range modes {
		for j, b := range modes {
			if (i >= 4 && j >= 4) || (i == 3 && j == 3) {
				continue
			}
			flags := append(append([]string{}, a...), b...)
			mustRun(t, 2, append([]string{"send", "POST", srv.URL}, flags...)...)
			mustRun(t, 2, append([]string{"run", "API/R"}, flags...)...)
			mustRun(t, 2, append([]string{"request", "create", "API/Invalid", "--method", "POST", "--url", srv.URL}, flags...)...)
		}
	}
	for _, flags := range [][]string{
		{"--body-file", "missing"}, {"--body-file", "."}, {"--body-file", ""}, {"--json", "@"},
		{"--form-file", "q="}, {"--urlencoded", "missing-equals"}, {"--form", "q=v", "-H", "Content-Type: application/json"},
		{"--form", "q=v", "-H", "Content-Type: multipart/form-data", "-H", "content-type: multipart/form-data"},
	} {
		mustRun(t, 2, append([]string{"send", "POST", srv.URL}, flags...)...)
	}
	if calls.Load() != 0 {
		t.Fatalf("invalid requests sent %d times", calls.Load())
	}
}

func TestBodyRedirectReplay(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("file.bin", []byte("exact\x00bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, status := range []int{307, 308} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			received := make(chan string, 2)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				data, _ := io.ReadAll(r.Body)
				if r.ContentLength != int64(len(data)) {
					t.Errorf("redirect request length %d != %d", r.ContentLength, len(data))
				}
				received <- r.Method + "|" + r.Header.Get("Content-Type") + "|" + string(data)
				if r.URL.Path == "/start" {
					http.Redirect(w, r, "/end", status)
				} else {
					io.WriteString(w, "ok")
				}
			}))
			defer srv.Close()
			for _, flags := range [][]string{{"--body-file", "file.bin"}, {"--form-file", "q=file.bin"}, {"--body", ""}} {
				got, _ := mustRun(t, 0, append([]string{"send", "POST", srv.URL + "/start"}, flags...)...)
				if got != "ok" {
					t.Fatal(got)
				}
				first, second := <-received, <-received
				if first != second {
					t.Fatal("redirect changed body or boundary")
				}
				if strings.Contains(first, "multipart/form-data") {
					_, params, err := mime.ParseMediaType(strings.SplitN(first, "|", 3)[1])
					if err != nil || params["boundary"] == "" {
						t.Fatal(first)
					}
				}
			}
		})
	}
}

func TestUploadCancellation(t *testing.T) {
	t.Chdir(t.TempDir())
	f, err := os.Create("large.bin")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(64 << 20); err != nil {
		t.Fatal(err)
	}
	f.Close()
	started, release := make(chan struct{}), make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.ReadFull(r.Body, make([]byte, 512))
		close(started)
		<-release
	}))
	defer srv.Close()
	defer close(release)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan int, 1)
	go func() {
		done <- Run(ctx, []string{"send", "POST", srv.URL, "--form-file", "file=large.bin"}, io.Discard, io.Discard)
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("upload did not start")
	}
	cancel()
	select {
	case code := <-done:
		if code != 130 {
			t.Fatalf("cancellation exit %d", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("upload did not cancel")
	}
}
