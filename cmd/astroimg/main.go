// Command astroimg builds, runs, and ships QEMU golden images across
// distros/layers/architectures. It replaces the project's former Makefile +
// shell pipeline with a single cross-platform, CI-ready binary.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	os.Exit(run())
}

// run holds the actual logic so `defer stop()` executes before the process
// exits -- os.Exit in main itself would skip any deferred calls.
func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := rootCmd.ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		return 1
	}

	return 0
}
