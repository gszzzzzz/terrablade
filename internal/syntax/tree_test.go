package syntax

import (
	"bytes"
	"strconv"
	"strings"
	"testing"
)

func TestFileOwnsSource(t *testing.T) {
	source := []byte("x + 1\n")
	original := bytes.Clone(source)
	file := parseExpressionSource(source)
	if !bytes.Equal(source, original) {
		t.Fatal("parsing changed caller source")
	}
	// Overwriting the caller's buffer must not reach the owned snapshot.
	for i := range source {
		source[i] = 'z'
	}
	assertTreeInvariants(t, original, file)
	child, ok := file.root.Child(0).Node()
	if !ok || child.Kind() != BinaryExpression {
		t.Fatal("first child must be a binary expression")
	}
}

func TestFileDeepInput(t *testing.T) {
	// The recursion guard stops grammar descent, and file recovery preserves
	// the rest without descending through every remaining balanced delimiter.
	source := []byte(strings.Repeat("[", 10000) + strings.Repeat("]", 10000))
	assertTreeInvariants(t, source, parseExpressionSource(source))
}

func TestSyntaxElementViews(t *testing.T) {
	file := parseExpressionSource([]byte("x"))
	root := file.root.Element()
	node, ok := root.Node()
	if !ok || node != file.root || node.Element() != root || root.Span() != file.root.Span() {
		t.Fatal("node and element views must identify the same tree position")
	}
	if rootToken, isToken := root.Token(); isToken || rootToken != (SyntaxToken{}) {
		t.Fatal("node element must not have a token view")
	}
	variable, ok := node.Child(0).Node()
	if !ok || variable.Kind() != VariableExpression {
		t.Fatal("first child must be a variable")
	}
	leaf := variable.Child(0)
	token, ok := leaf.Token()
	if !ok || token.Kind() != Identifier || token.Span() != (Span{Start: 0, End: 1}) || leaf.Span() != token.Span() {
		t.Fatal("token view must preserve kind and span")
	}
	if leafNode, isNode := leaf.Node(); isNode || leafNode != (SyntaxNode{}) {
		t.Fatal("token element must not have a node view")
	}
	// Token views are independent values even though the arena reuses lexer data.
	token.kind = Invalid
	stored, _ := leaf.Token()
	if token.Kind() != Invalid || stored.Kind() != Identifier {
		t.Fatal("changing a token value changed stored tree data")
	}
	eof, ok := file.root.Child(1).Token()
	if !ok || eof.Kind() != EOF || eof.Span() != (Span{Start: 1, End: 1}) {
		t.Fatal("empty EOF span must still have a valid token view")
	}
}

func TestSyntaxZeroValues(t *testing.T) {
	var element SyntaxElement
	if node, ok := element.Node(); ok || node != (SyntaxNode{}) {
		t.Fatal("zero element must not have a node view")
	}
	if token, ok := element.Token(); ok || token != (SyntaxToken{}) {
		t.Fatal("zero element must not have a token view")
	}
	var node SyntaxNode
	if element.Span() != (Span{}) || node.Span() != (Span{}) || node.Kind() != InvalidNode || node.ChildCount() != 0 || node.Element() != element {
		t.Fatal("zero handles must have empty, invalid views")
	}
}

func TestSyntaxChildBounds(t *testing.T) {
	root := parseExpressionSource([]byte("x")).root
	for _, node := range []SyntaxNode{{}, root} {
		for _, index := range []int{-1, node.ChildCount(), node.ChildCount() + 1} {
			t.Run(strconv.Itoa(node.ChildCount())+"/"+strconv.Itoa(index), func(t *testing.T) {
				defer func() {
					if recover() == nil {
						t.Fatal("out-of-range child access did not panic")
					}
				}()
				node.Child(index)
			})
		}
	}
}

func TestArenaReusesTokensAndStoresEachEdgeOnce(t *testing.T) {
	source := []byte(" /*head*/ f(a + b, g(c.d), e[ /*key*/ k ], [for x in xs : {a=x}], \"%{if a}${x}%{else}y%{endif}\") #tail\n")
	p := newParser(source)
	tokens := p.tokens
	root := p.begin()
	p.consumeUntil(&root, p.look(newlineTransparent))
	p.operand(&root, 0, newlineTerminates)
	file := p.file(root)
	assertTreeInvariants(t, source, file)
	arena := file.root.arena
	if &arena.tokens[0] != &tokens[0] || len(arena.tokens) != len(tokens) {
		t.Fatal("arena must reuse lexer token storage")
	}
	if len(p.pending) != 0 {
		t.Fatal("finished file left pending children")
	}
	// A tree has one incoming edge for every element except its root. This
	// catches duplicated child ranges left behind when wrapping expressions.
	if len(arena.children) != len(arena.nodes)+len(arena.tokens)-1 {
		t.Fatal("arena must store each tree edge exactly once")
	}
	end := 0
	for _, node := range arena.nodes {
		if node.firstChild != end {
			t.Fatal("node child ranges must partition the flat child storage")
		}
		end += node.childCount
	}
	if end != len(arena.children) {
		t.Fatal("unowned child references in arena")
	}
}

func TestArenaHandlesSurviveGrowthAndOtherParses(t *testing.T) {
	source := []byte("x + " + strings.Repeat("y + ", 1000) + "z")
	p := newParser(source)
	root := p.begin()
	first := p.prefix(newlineTerminates)
	leaf := first.Child(0)
	root.node(first)
	// Finishing the remainder grows the same arena after handles were taken.
	file := p.file(root)
	_ = parseExpressionSource([]byte("other"))
	if first.Kind() != VariableExpression || first.Span() != (Span{Start: 0, End: 1}) || first.Child(0) != leaf {
		t.Fatal("arena growth or another parse changed an existing handle")
	}
	if token, ok := leaf.Token(); !ok || token.Kind() != Identifier || token.Span() != first.Span() {
		t.Fatal("retained token handle changed")
	}
	assertTreeInvariants(t, source, file)
}

func TestArenaParseAllocationScaling(t *testing.T) {
	measure := func(terms int) float64 {
		source := []byte(strings.Repeat("x + ", terms-1) + "x")
		return testing.AllocsPerRun(10, func() {
			_ = parseExpressionSource(source)
		})
	}
	small, large := measure(100), measure(1000)
	// Slice growth may vary with the Go runtime. Allow ample growth overhead,
	// but not an allocation per node/token or per-node child buffer.
	if large > 2*small+16 {
		t.Fatalf("10x more terms grew allocations from %g to %g; want amortized storage growth", small, large)
	}
}

func TestDeepArenaTraversalAllocations(t *testing.T) {
	terms := maxRecursiveExpressionDepth * 8
	source := []byte(strings.Repeat("x + ", terms-1) + "x")
	file := parseExpressionSource(source)
	// The caller owns a reusable traversal stack; node/token access must add no
	// allocations even when visiting the full deep tree, including every trivia.
	stack := make([]SyntaxElement, 0, terms*8)
	var nodes, tokens, width int
	allocations := testing.AllocsPerRun(10, func() {
		nodes, tokens, width = countTree(file.root, stack)
	})
	if nodes != terms*2 || tokens != terms*4-2 || width != len(source) {
		t.Fatalf("traversal counted %d nodes, %d tokens, %d bytes", nodes, tokens, width)
	}
	if allocations != 0 {
		t.Fatalf("full traversal allocated %g times, want zero", allocations)
	}
}

func BenchmarkParseExpressionChain(b *testing.B) {
	for _, terms := range []int{100, 1000, maxRecursiveExpressionDepth * 8} {
		b.Run(strconv.Itoa(terms), func(b *testing.B) {
			source := []byte(strings.Repeat("x + ", terms-1) + "x")
			b.ReportAllocs()
			b.SetBytes(int64(len(source)))
			for b.Loop() {
				_ = parseExpressionSource(source)
			}
		})
	}
}

func BenchmarkTraverseDeepExpression(b *testing.B) {
	terms := maxRecursiveExpressionDepth * 8
	file := parseExpressionSource([]byte(strings.Repeat("x + ", terms-1) + "x"))
	stack := make([]SyntaxElement, 0, terms*8)
	b.ReportAllocs()
	for b.Loop() {
		countTree(file.root, stack)
	}
}
