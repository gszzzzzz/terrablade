package terrablade

import (
	"go/ast"
	"go/constant"
	"go/parser"
	"go/token"
	"go/types"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gszzzzzz/terrablade/internal/syntax"
)

func TestDiagnosticMappingExhaustive(t *testing.T) {
	declared := declaredDiagnosticKinds(t)
	seen := make(map[DiagnosticKind]bool)
	for kind := syntax.DiagnosticKind(0); kind < syntax.DiagnosticKindCount; kind++ {
		mapped := publicDiagnosticKind(kind)
		if _, ok := declared[mapped]; !ok {
			t.Errorf("%s maps to undeclared public kind %q", kind, mapped)
		}
		if seen[mapped] {
			t.Errorf("multiple internal kinds map to %q", mapped)
		}
		seen[mapped] = true
	}
	for kind, name := range declared {
		if !seen[kind] {
			t.Errorf("public constant %s (%q) has no internal mapping", name, kind)
		}
	}
}

func TestUnknownDiagnosticKindPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("an unknown internal kind escaped the public conversion")
		}
	}()
	newParseError("", []syntax.Diagnostic{{Kind: syntax.DiagnosticKindCount}})
}

// Inspect declarations instead of maintaining a second hand-written list that
// could forget the same new constant as the production mapping. Type-check only
// constants and DiagnosticKind: no imports or internal implementation are needed.
func declaredDiagnosticKinds(t *testing.T) map[DiagnosticKind]string {
	t.Helper()
	filenames, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	constants := &ast.File{Name: ast.NewIdent("terrablade")}
	for _, filename := range filenames {
		if strings.HasSuffix(filename, "_test.go") {
			continue
		}
		file, parseErr := parser.ParseFile(fset, filename, nil, 0)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		for _, declaration := range file.Decls {
			group, ok := declaration.(*ast.GenDecl)
			if !ok {
				continue
			}
			if group.Tok == token.CONST {
				constants.Decls = append(constants.Decls, group)
			}
			if group.Tok == token.TYPE {
				for _, spec := range group.Specs {
					if spec.(*ast.TypeSpec).Name.Name == "DiagnosticKind" {
						constants.Decls = append(constants.Decls, &ast.GenDecl{Tok: token.TYPE, Specs: []ast.Spec{spec}})
					}
				}
			}
		}
	}
	pkg, err := new(types.Config).Check("terrablade", fset, []*ast.File{constants}, nil)
	if err != nil {
		t.Fatal(err)
	}
	declared := make(map[DiagnosticKind]string)
	diagnosticType := pkg.Scope().Lookup("DiagnosticKind").Type()
	for _, name := range pkg.Scope().Names() {
		value, ok := pkg.Scope().Lookup(name).(*types.Const)
		if !ok || !value.Exported() || !types.Identical(value.Type(), diagnosticType) {
			continue
		}
		kind := DiagnosticKind(constant.StringVal(value.Val()))
		if previous, exists := declared[kind]; exists {
			t.Errorf("public constants %s and %s duplicate %q", previous, name, kind)
		}
		declared[kind] = name
	}
	return declared
}
