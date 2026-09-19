// Command req is the process entry point: it wires the interrupt-aware
// context and real streams into cli.Run and exits with its code.
package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/SwastikGorai/req/internal/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	os.Exit(cli.Run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}
