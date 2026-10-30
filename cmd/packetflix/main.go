// (c) Siemens AG 2023
//
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"os"
	"os/signal"
	"runtime"
	"syscall"
)

func main() {
	// Not strictly necessary, but makes life easier for lxkns/Ghostwire:
	// without it, we might end up with the situation where the OS-level thread
	// from which we started the dumpcap binary and which we thus switched into
	// the target's network namespace ends up being the main thread and thus the
	// process itself. Now that then makes lxkns see packetflix' process being
	// the ealdorman in a network namespace different from its container's
	// network namespace.
	runtime.LockOSThread()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// Cobra already reports any error, see also:
	// https://github.com/spf13/cobra/issues/304
	if err := newRootCmd().ExecuteContext(ctx); err != nil {
		stop()
		os.Exit(1)
	}
}
