package lowering

import (
	"errors"

	"github.com/gszzzzzz/terrablade/internal/document"
	"github.com/gszzzzzz/terrablade/internal/syntax"
)

// Expression lowers one expression without its enclosing body, so tests can
// lower, render, and fuzz expressions in isolation. Unlike File it reports
// misuse as an error: a result with diagnostics, or a node that is not a
// complete expression.
func Expression(result syntax.Result, node syntax.Node) (document.Doc, error) {
	if len(result.Diagnostics()) != 0 {
		return document.Doc{}, errors.New("lowering: cannot format a result with diagnostics")
	}
	switch node.Kind() {
	case syntax.LiteralExpression, syntax.VariableExpression,
		syntax.ParenthesizedExpression, syntax.UnaryExpression,
		syntax.BinaryExpression, syntax.ConditionalExpression,
		syntax.FunctionCallExpression, syntax.TraversalExpression,
		syntax.TupleExpression, syntax.ObjectExpression, syntax.ForExpression,
		syntax.TemplateExpression:
		return lowerExpression(result, node).doc, nil
	}
	return document.Doc{}, errors.New("lowering: expected an expression node")
}

// Lowering's own levels, above the parser's operators, for
// TestBindingPowersMatchParserPrecedence.
const (
	TraversalPower = traversalPower
	AtomicPower    = atomicPower
)
