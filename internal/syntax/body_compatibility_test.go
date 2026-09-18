package syntax

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// Acceptance follows the upstream native parser, including its EOF and BOM
// extensions and its stricter single-line block rules. The optional OpenTofu
// check below exercises the same corpus without adding a library dependency.
var bodyCompatibilityCases = []struct {
	name   string
	source string
	valid  bool
}{
	{"empty", "", true},
	{"trivia only", " \t# c\r\n/*x*/ // eof", true},
	{"attribute EOF", "a=1", true},
	{"attribute newline", "a=1\n", true},
	{"attributes CRLF", "a=1\r\nb=2\r\n", true},
	{"attribute comments", "a/*name*/=/*value*/1 #tail\nb=2", true},
	{"block comment inline", "a=1/*\n*/+2\nb=3", true},
	{"parenthesized continuation", "a=(1\n+2)\nb=3", true},
	{"object expression", "a={\nx=1\ny=2\n}\nb=3", true},
	{"tuple and for expressions", "a=[for x in xs : {name=x}]\nb=[1,2]", true},
	{"template expression", "a=\"%{if x}${x}%{else}none%{endif}\"", true},
	{"heredoc expression", "a=<<E\n${x}\nE\nb=2", true},
	{"empty block", "b {}", true},
	{"empty block multiline", "b {\n}", true},
	{"block with comments", "b/*head*/{/*empty*/}", true},
	{"single attribute", "b { a=1 }", true},
	{"single attribute nested expression", "b { a={x=1\ny=2} }", true},
	{"single attribute parenthesized", "b { a=(1\n+2) }", true},
	{"single attribute multiline inline comment", "b {/*\n*/a=1/*\n*/}", true},
	{"block opening line comment", "b {# opening\n a=1\n}", true},
	{"nested block", "a {\n b {}\n}", true},
	{"mixed body", "a=1\nb { c=2 }\nd {}\ne=3", true},
	{"unquoted labels", "resource aws_instance web {}", true},
	{"quoted labels", "b \"x\" \"y\" {}", true},
	{"empty quoted label", "b \"\" {}", true},
	{"adjacent quoted labels", "b\"x\"\"y\"{}", true},
	{"escaped labels", "b \"x\\n\\u0041\\\"\" {}", true},
	{"escaped template labels", "b \"$${x}%%{if x}\" {}", true},
	{"unicode names", "설정 이름 { 값=1 }", true},
	{"keyword names", "true { false=null }\nfor in {}", true},
	{"same attribute in distinct scopes", "a=1\nb { a=2 }\nb { a=3 }", true},
	{"duplicate blocks", "b x {}\nb x {}", true},
	{"attribute and block same name", "b=1\nb {}", true},
	{"case distinct names", "a=1\nA=2", true},
	{"unnormalized distinct identifiers", "é=1\né=2", true},
	{"leading BOM", "\ufeffa=1", true},
	{"only BOM", "\ufeff", true},
	{"missing attribute value", "a=\nb=2", false},
	{"value on next line", "a=\n1", false},
	{"missing equal", "a\nb=2", false},
	{"colon attribute", "a:1", false},
	{"quoted attribute name", "\"a\"=1", false},
	{"numeric attribute name", "1=2", false},
	{"comma between attributes", "a=1,b=2", false},
	{"space between attributes", "a=1 b=2", false},
	{"semicolon separator", "a=1;b=2", false},
	{"physical newline in inline comment is not separator", "a=1/*\n*/b=2", false},
	{"duplicate attributes", "a=1\na=2", false},
	{"duplicate nested attributes", "b {\na=1\na=2\n}", false},
	{"bare header", "b", false},
	{"header newline", "b\n{}", false},
	{"label newline", "b x\n{}", false},
	{"label equals", "b x = {}", false},
	{"number label", "b 1 {}", false},
	{"parenthesized label", "b (x) {}", false},
	{"heredoc label", "b <<E\nx\nE\n{}", false},
	{"interpolated label", "b \"${x}\" {}", false},
	{"constant interpolated label", "b \"${\"x\"}\" {}", false},
	{"directive label", "b \"%{if x}a%{endif}\" {}", false},
	{"malformed label escape", "b \"\\q\" {}", false},
	{"literal label newline", "b \"x\ny\" {}", false},
	{"unterminated label", "b \"x", false},
	{"unclosed block", "b {\na=1\n", false},
	{"unclosed single line block", "b { a=1", false},
	{"extra closer", "b {}\n}", false},
	{"foreign closer", "b {\n]\n}", false},
	{"two blocks same line", "b {} c {}", false},
	{"nested block same line", "b { c {} }", false},
	{"nested closer same line", "b {\nc {}}", false},
	{"attribute closes multiline block on same line", "b {\na=1}", false},
	{"two attributes in single line block", "b {a=1 b=2}", false},
	{"comma in single line block", "b {a=1,b=2}", false},
	{"single line attribute ends with newline", "b {a=1\n}", false},
	{"single line attribute comment ends with newline", "b {a=1# end\n}", false},
	{"single line heredoc", "b {a=<<E\nx\nE\n}", false},
	{"misplaced BOM", " \ufeffa=1", false},
	{"two BOMs", "\ufeff\ufeffa=1", false},
	{"BOM between items", "a=1\n\ufeffb=2", false},
}

func TestBodyCompatibility(t *testing.T) {
	for _, test := range bodyCompatibilityCases {
		t.Run(test.name, func(t *testing.T) {
			file := parseBodySource([]byte(test.source))
			assertExpressionPartition(t, []byte(test.source), file)
			if valid := len(file.diagnostics) == 0; valid != test.valid {
				t.Fatalf("valid = %v, want %v: %+v", valid, test.valid, file.diagnostics)
			}
		})
	}
}

func TestBodyOpenTofuCompatibility(t *testing.T) {
	if os.Getenv("TERRABLADE_COMPARE_TOFU") != "1" {
		t.Skip("set TERRABLADE_COMPARE_TOFU=1 to compare with an installed OpenTofu")
	}
	tofu, err := exec.LookPath("tofu")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range bodyCompatibilityCases {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, tofu, "fmt", "-no-color", "-")
			command.Stdin = strings.NewReader(test.source)
			output, err := command.CombinedOutput()
			if ctx.Err() != nil {
				t.Fatal(ctx.Err())
			}
			if err != nil {
				if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 2 {
					t.Fatalf("unexpected OpenTofu failure: %v\n%s", err, output)
				}
			}
			accepted := len(parseBodySource([]byte(test.source)).diagnostics) == 0
			if (err == nil) != test.valid || (err == nil) != accepted {
				t.Fatalf("OpenTofu accepted=%v, parser accepted=%v, want=%v\n%s", err == nil, accepted, test.valid, output)
			}
		})
	}
}
