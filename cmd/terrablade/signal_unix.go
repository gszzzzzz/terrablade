//go:build unix

package main

import (
	"errors"
	"os/signal"
	"syscall"
)

func ignoreBrokenPipe() {
	// Go normally exits on SIGPIPE for stdout and stderr before Write can
	// return EPIPE; ignoring it lets run report the failure with exit 2.
	signal.Ignore(syscall.SIGPIPE)
}

// isBrokenPipe reports a write to a pipe whose reader has exited.
func isBrokenPipe(err error) bool { return errors.Is(err, syscall.EPIPE) }
