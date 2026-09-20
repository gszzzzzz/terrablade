// Command terrablade formats native HCL from stdin or explicit file paths.
package main

import "os"

func main() {
	ignoreBrokenPipe()
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
