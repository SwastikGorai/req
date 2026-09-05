// Package scripting embeds the JavaScript runtime. Phase 1 is a feasibility
// spike: it proves ownership, interruption and asynchronous host work, and
// claims no Postman API compatibility yet.
package scripting

import "context"

// Source is one script to execute. Name appears in error locations.
type Source struct {
	Name string
	Code string
}

// Report is what a completed run hands back. It grows only when production
// bindings are implemented.
type Report struct {
	Logs []string
}

// Engine runs scripts. Run blocks until the script and all of its tracked
// asynchronous work have settled, or ctx ends. Runs are sequential; Close
// releases the runtime and discards late completions.
type Engine interface {
	Run(ctx context.Context, src Source) (Report, error)
	Close() error
}
