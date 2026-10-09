package main

import (
	"runtime/debug"
	"strings"
)

// version is the release version, set at link time with
// -ldflags "-X main.version=0.1.0". Builds that leave it empty, such as
// go install, fall back to the module version Go records in the binary.
var version string

// versionText is the --version output. A leading "v" is dropped so release
// archives and go install builds of the same tag print the same version.
func versionText() string {
	v := version
	if v == "" {
		if info, ok := debug.ReadBuildInfo(); ok {
			v = info.Main.Version
		}
	}
	if v == "" {
		v = "(devel)"
	}
	return "terrablade " + strings.TrimPrefix(v, "v") + "\n"
}
