package syntax

// This file holds test seams: entry points that exist only so tests can drive
// one production in isolation. They live here rather than in the production
// files so that no shipped code path can reach them.

// parseExpressionSource is an internal test seam for an attribute-style value:
// unparenthesized newlines terminate it. It is not a configuration-file parser.
// Leading and trailing trivia belong to File. Remaining non-trivia is an error.
func parseExpressionSource(source []byte) Result {
	p := newParser(source)
	root := p.begin()
	p.consumeUntil(&root, p.look(newlineTransparent))
	p.operand(&root, lowestPower, newlineTerminates)
	return p.file(root)
}
