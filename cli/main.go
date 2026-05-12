package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/kurrent-io/gaffer/cli/cmd"
	"github.com/kurrent-io/gaffer/cli/internal/telemetry"
	"github.com/kurrent-io/gaffer/cli/internal/userconfig"
)

// flushTimeout bounds how long process exit waits for in-flight
// telemetry sends to drain. Sized comfortably above the 2s per-send
// default so the per-send budget can elapse without flush preempting
// it; on a quiet run (no events emitted) wg.Wait returns instantly
// so this is the worst case, not the typical case.
//
// In-flight emits are concurrent goroutines (one per envelope) so
// many can drain inside this window in parallel.
const flushTimeout = 5 * time.Second

func main() {
	rootCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	client := buildClient(os.Stderr)
	ctx := telemetry.WithClient(rootCtx, client)

	err := cmd.Execute(ctx)

	flushCtx, cancel := context.WithTimeout(context.Background(), flushTimeout)
	_ = client.Flush(flushCtx)
	cancel()

	if err != nil {
		os.Exit(1)
	}
}

// buildClient resolves opt-out and identity from the user's config
// and constructs the per-process telemetry Client. Returns nil when
// opt-out is active, when the config is unreadable, or when identity
// resolution fails - all "telemetry off for this run" signals.
//
// Errors are swallowed silently: telemetry is best-effort and main
// has no useful surface to report config-read failures on without
// surprising users with errors about a feature they haven't asked
// for. Users who want to inspect the state run
// `gaffer config telemetry status`.
func buildClient(noticeOut io.Writer) *telemetry.Client {
	store, err := userconfig.Open()
	if err != nil {
		return nil
	}
	cwd, _ := os.Getwd()
	home, _ := os.UserHomeDir()
	return telemetry.StartupGate(store, cwd, home, noticeOut,
		telemetry.WithUserAgent(userAgent()),
		telemetry.WithLibVersion(cmd.Version),
	)
}

// userAgent stamps a structured per-request identifier so the
// ingest worker can break events down by release, OS, and arch.
// Format mirrors Go's net/http default: `<product>/<version>
// (<os>; <arch>; <runtime>)`. Read at startup once and stamped on
// every emit's HTTP request.
func userAgent() string {
	return fmt.Sprintf("gaffer-cli/%s (%s; %s; %s)",
		cmd.Version, runtime.GOOS, runtime.GOARCH, runtime.Version())
}
