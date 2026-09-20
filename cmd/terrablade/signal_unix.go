//go:build unix

package main

import (
	"os/signal"
	"syscall"
)

func ignoreBrokenPipe() {
	// The CLI reports stdout/stderr write failures with exit 2. Go normally
	// terminates on SIGPIPE for these descriptors before Write can return EPIPE.
	// Keep this process-wide policy in main, outside the reusable runner.
	signal.Ignore(syscall.SIGPIPE)
}
