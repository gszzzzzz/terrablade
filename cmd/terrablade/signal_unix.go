//go:build unix

package main

import (
	"errors"
	"os/signal"
	"syscall"
)

func ignoreBrokenPipe() {
	// The CLI reports stdout/stderr write failures with exit 2. Go normally
	// terminates on SIGPIPE for these descriptors before Write can return EPIPE.
	// Keep this process-wide policy in main, outside the reusable runner.
	signal.Ignore(syscall.SIGPIPE)
}

// isBrokenPipe reports a write to a pipe whose reader has exited.
func isBrokenPipe(err error) bool { return errors.Is(err, syscall.EPIPE) }
