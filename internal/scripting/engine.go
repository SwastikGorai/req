// Package scripting embeds the JavaScript runtime. It began as a feasibility
// spike proving ownership, interruption and asynchronous host work; the
// production surface (pm.execution, console) now lives here too, while the
// full Postman API (pm.response, pm.variables, assertions, pm.sendRequest)
// arrives with later phases.
package scripting

import "context"

// Source is one script to execute. Name appears in error locations.
type Source struct {
	Name string
	Code string
}

// Report is what a completed run hands back.
type Report struct {
	Logs []string
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
