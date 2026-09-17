// Package scripting embeds the JavaScript runtime. It began as a feasibility
// spike proving ownership, interruption and asynchronous host work; the
// production pm surface (variables, request, response, execution) and console
// live in bindings.go, assertions in assertions.go, and callback HTTP work in
// async.go.
package scripting

import "context"

// Source is one script to execute. Name appears in error locations.
type Source struct {
	Name string
	Code string
}

// TestResult is one named pm.test outcome.
type TestResult struct {
	Name   string
	Failed bool
	Error  string // the thrown error's message, first line only, when Failed
}

// Report is what a completed run hands back.
type Report struct {
	Logs []string
	// Tests holds the pm.test outcomes in call order. Failed tests do not
	// fail the run; the execution turns them into exit code 6.
	Tests []TestResult
	// Skipped reports that the script called pm.execution.skipRequest(),
	// even when it caught the resulting sentinel.
	Skipped bool
}

// Engine runs scripts. Run blocks until the script and all of its tracked
// asynchronous work have settled, or ctx ends. Runs are sequential; Close
// releases the runtime and discards late completions.
type Engine interface {
	Run(ctx context.Context, src Source) (Report, error)
	Close() error
}
