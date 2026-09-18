package terrablade_test

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"

	"terrablade"
)

func TestIndentWidthLimits(t *testing.T) {
	const source = "b {\n c { a=1 }\n}\n"
	for _, width := range []int{1, 16} {
		options := terrablade.Options{IndentWidth: width}
		got := format(t, []byte(source), options)
		want := "b {\n" + strings.Repeat(" ", width) + "c {\n" +
			strings.Repeat(" ", 2*width) + "a = 1\n" + strings.Repeat(" ", width) + "}\n}\n"
		if string(got) != want {
			t.Fatalf("IndentWidth %d: got %q, want %q", width, got, want)
		}
		if again := format(t, got, options); !bytes.Equal(again, got) {
			t.Fatalf("IndentWidth %d is not idempotent: %q => %q", width, got, again)
		}
	}
	for _, width := range []int{17, int(^uint(0) >> 1)} {
		for _, source := range []string{"", source, "a=", "\xff"} {
			output, err := terrablade.Format([]byte(source), terrablade.Options{IndentWidth: width})
			var optionError *terrablade.OptionsError
			if output != nil || !errors.As(err, &optionError) || optionError.Option != "IndentWidth" || optionError.Value != width {
				t.Fatalf("IndentWidth %d: got %q, %v; want nil and OptionsError", width, output, err)
			}
			want := fmt.Sprintf("terrablade: IndentWidth must not exceed 16 (got %d)", width)
			if err.Error() != want {
				t.Fatalf("Error() = %q, want %q", err.Error(), want)
			}
		}
	}
	// Validation order is stable even when errors include different constraints.
	for _, test := range []struct {
		options terrablade.Options
		want    string
	}{
		{terrablade.Options{PrintWidth: -1, IndentWidth: 17}, "terrablade: PrintWidth must not be negative (got -1)"},
		{terrablade.Options{IndentWidth: 17, TabWidth: -1}, "terrablade: IndentWidth must not exceed 16 (got 17)"},
		{terrablade.Options{IndentWidth: -1}, "terrablade: IndentWidth must not be negative (got -1)"},
	} {
		_, err := terrablade.Format([]byte("a="), test.options)
		if err == nil || err.Error() != test.want {
			t.Fatalf("validation precedence: got %v, want %q", err, test.want)
		}
	}
}

func TestTabWidthLimits(t *testing.T) {
	const source = "b {\n a=f(\"\t\t\",beta)\n}\n"
	for _, width := range []int{1, 16} {
		options := terrablade.Options{TabWidth: width}
		got := format(t, []byte(source), options)
		if string(got) != "b {\n  a = f(\"\t\t\", beta)\n}\n" {
			t.Fatalf("TabWidth %d changed literal tabs: %q", width, got)
		}
		if again := format(t, got, options); !bytes.Equal(again, got) {
			t.Fatalf("TabWidth %d is not idempotent: %q => %q", width, got, again)
		}
	}
	for _, width := range []int{17, int(^uint(0) >> 1)} {
		for _, source := range []string{"", source, "a=", "\xff"} {
			output, err := terrablade.Format([]byte(source), terrablade.Options{TabWidth: width})
			var optionError *terrablade.OptionsError
			if output != nil || !errors.As(err, &optionError) || optionError.Option != "TabWidth" || optionError.Value != width {
				t.Fatalf("TabWidth %d: got %q, %v; want nil and OptionsError", width, output, err)
			}
			want := fmt.Sprintf("terrablade: TabWidth must not exceed 16 (got %d)", width)
			if err.Error() != want {
				t.Fatalf("Error() = %q, want %q", err.Error(), want)
			}
		}
	}
}

func TestMaxIntPrintWidth(t *testing.T) {
	options := terrablade.Options{PrintWidth: int(^uint(0) >> 1), IndentWidth: 16, TabWidth: 16}
	got := format(t, []byte("b {\n a=f(\"\t\t\",beta)\n}\n"), options)
	want := "b {\n" + strings.Repeat(" ", 16) + "a = f(\"\t\t\", beta)\n}\n"
	if string(got) != want {
		t.Fatalf("MaxInt PrintWidth: got %q, want %q", got, want)
	}
	if again := format(t, got, options); !bytes.Equal(again, got) {
		t.Fatalf("MaxInt PrintWidth is not idempotent: %q => %q", got, again)
	}
}
