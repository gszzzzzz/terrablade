package lowering

import "github.com/gszzzzzz/terrablade/internal/syntax"

// The binding-power ladder duplicates the parser's precedence, but the
// parser's own table is unexported and lives in another package, so nothing
// links the two. TestBindingPowersMatchParserPrecedence derives the parser's
// ordering from the shapes syntax.Parse produces and compares it against
// these values; they are exported only for that test.
func BinaryPower(kind syntax.TokenKind) int { return binaryPower(kind) }

const (
	ConditionalPower = conditionalPower
	UnaryPower       = unaryPower
	TraversalPower   = traversalPower
	AtomicPower      = atomicPower
)
