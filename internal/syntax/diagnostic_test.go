package syntax

import (
	"strings"
	"testing"
)

func TestDiagnosticMessageCompleteness(t *testing.T) {
	for kind := DiagnosticKind(0); kind < DiagnosticKindCount; kind++ {
		message := kind.Message()
		if message == "" || message == "Unknown diagnostic." || message == kind.String() {
			t.Errorf("%s has no user-facing message: %q", kind, message)
			continue
		}
		if message[0] < 'A' || message[0] > 'Z' || !strings.HasSuffix(message, ".") || strings.ContainsAny(message, "\r\n\t") {
			t.Errorf("%s message is not a standalone sentence: %q", kind, message)
		}
	}
	for value := int(DiagnosticKindCount); value <= 255; value++ {
		if got := DiagnosticKind(value).Message(); got != "Unknown diagnostic." {
			t.Errorf("unknown DiagnosticKind(%d).Message() = %q", value, got)
		}
	}
}
