package terrablade_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"testing"
	"time"

	"terrablade"
)

func TestOpenTofuFixedPoints(t *testing.T) {
	if os.Getenv("TERRABLADE_COMPARE_TOFU") != "1" {
		t.Skip("set TERRABLADE_COMPARE_TOFU=1 to compare with an installed OpenTofu")
	}
	// These fixtures use upstream's indentation convention. Custom indentation
	// and the narrow exceptions documented in doc.go are not upstream fixed points.
	for _, source := range []string{
		"", "\ufeffa=1\r\nlong=2\r\n", "#\r", "a=1 #x\r\r\nb=2",
		"resource aws_instance web {\n ami=\"${var.ami}\"\n instance_type=\"small\"\n\n tags={name=\"web\",owner=\"ops\"}\n}\n",
		`block /*type*/ bare /*label*/ "q" {}`, `a="${foo.0.bar}"`,
		`a={"${name}"="${value}"}`, `a={"${"k"}"=1}`, `a=-"${x+y}"`,
		`a="${"${x}"}"`, `a=(foo[*].b)[0]`, `a=foo[*].b[0]`, `a=foo[*].0.bar`,
		"a=f(alpha+beta,ready?yes:no)", "a=[for x in xs:x.id if x.enabled]",
		"a={for k,v in xs:k=>v...}", "a=<<-E\n  literal\n  E\n",
		"b {\n a=\"${<<E\nvalue\nE\n}\" /*tail*/\n}\n",
		`a="prefix ${~{x=1}~}"`, `a="%{for x in xs}${x}%{endfor}"`,
	} {
		for _, width := range []int{16, 80} {
			output := format(t, []byte(source), terrablade.Options{PrintWidth: width})
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			command := exec.CommandContext(ctx, "tofu", "fmt", "-no-color", "-")
			command.Stdin = bytes.NewReader(output)
			formatted, err := command.CombinedOutput()
			cancel()
			if err != nil {
				t.Fatalf("OpenTofu rejected %q: %v\n%s", output, err, formatted)
			}
			if !bytes.Equal(formatted, output) {
				t.Errorf("OpenTofu changed canonical output: %q => %q", output, formatted)
			}
		}
	}
}
