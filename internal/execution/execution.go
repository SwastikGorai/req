// Package execution resolves a saved request plus CLI overrides into one
// outgoing HTTP request and executes it. It is the single execution path
// shared by `req send` and `req run`.
package execution

import (
	"req/internal/httpclient"
	"req/internal/model"
	"req/internal/variables"
	"time"
)

// Outgoing is one fully resolved outgoing request plus execution policy.
type Outgoing struct {
	Method          string
	URL             string           // final URL, queries already appended
	Headers         [][2]string      // ordered, duplicates preserved
	Body            *httpclient.Body // nil = no body; opened only during execution
	Timeout         time.Duration
	InsecureTLS     bool
	FollowRedirects bool
	FailOnHTTPError bool
}

// Overrides are structural CLI overrides applied to an execution copy of a
// saved request; empty fields keep the saved value.
type Overrides struct {
	Auth        *model.Auth
	Method, URL string
	Queries     [][2]string // appended AFTER saved query entries
	Headers     [][2]string // appended AFTER saved header entries
	Body        *model.Body // nil keeps the saved body
}

// Policy is the per-execution HTTP policy from CLI flags.
type Policy struct {
	BodyBase        string // saved request paths use the workspace root; direct send uses cwd
	Variables       *variables.Scope
	Timeout         time.Duration
	InsecureTLS     bool
	FollowRedirects bool
	FailOnHTTPError bool
}
