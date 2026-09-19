package cli

import (
	"io"
	"os"

	"req/internal/output"
	"req/internal/store"
)

// stdoutIsTerminal is injectable so output behavior can be tested without a
// real terminal. Pipes and buffers deliberately report false.
var stdoutIsTerminal = func(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func prepareOutput(opts *output.Options, stdout io.Writer, ws *store.Workspace) error {
	opts.Terminal = stdoutIsTerminal(stdout)
	if ws == nil || (!opts.Verbose && !opts.JSON()) {
		return nil
	}
	var err error
	opts.SecretHeaders, err = ws.SecretHeaders()
	return err
}
