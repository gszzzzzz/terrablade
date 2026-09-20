//go:build !unix

package main

// Non-Unix platforms report broken streams as ordinary write errors.
func ignoreBrokenPipe() {}
