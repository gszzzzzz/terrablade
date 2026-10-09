package main

import "testing"

func TestVersionText(t *testing.T) {
	for _, test := range []struct{ linked, want string }{
		{"0.1.0", "terrablade 0.1.0\n"},
		{"v0.1.0", "terrablade 0.1.0\n"},
		{"0.2.0-rc.1", "terrablade 0.2.0-rc.1\n"},
	} {
		t.Run(test.linked, func(t *testing.T) {
			saved := version
			t.Cleanup(func() { version = saved })
			version = test.linked
			if got := versionText(); got != test.want {
				t.Fatalf("versionText() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestVersionTextWithoutLinkedVersion(t *testing.T) {
	saved := version
	t.Cleanup(func() { version = saved })
	version = ""
	// The fallback depends on how the test binary was built; it must still
	// name a version on a single line.
	got := versionText()
	if len(got) <= len("terrablade \n") || got[len(got)-1] != '\n' {
		t.Fatalf("versionText() = %q", got)
	}
}
