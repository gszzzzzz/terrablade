//go:build !unix

package main

// Non-Unix platforms report broken streams as ordinary write errors.
func ignoreBrokenPipe() {}

// isBrokenPipe is false here: other platforms report no portable broken-pipe
// error, so the write error is shown as it is.
func isBrokenPipe(error) bool { return false }
