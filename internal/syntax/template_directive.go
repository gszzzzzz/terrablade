package syntax

// templateDirective parses only a %{...} header and returns it with its
// keyword, or an empty name when the keyword is missing or unknown. Pairing
// the header with a body is a separate, iterative step, and malformed headers
// still retain their keyword so a later matching ending can recover the
// surrounding template structure.
func (p *parser) templateDirective() (SyntaxNode, string) {
	b := p.begin()
	p.templateSequenceOpen(&b)
	keyword := p.tokens[p.look(delimitedExpression)]
	if keyword.kind != Identifier {
		p.report(ExpectedTemplateDirective, keyword.span)
		p.recoverUntil(&b, delimitedExpression, templateBoundaries)
		p.templateSequenceEnd(&b)
		return b.finish(TemplateDirective), ""
	}

	p.consumeLookahead(&b, delimitedExpression)
	name := p.source[keyword.span.Start:keyword.span.End]
	switch name {
	case "if":
		p.operand(&b, lowestPower, delimitedExpression)
	case "for":
		if !p.forIntroduction(&b) {
			p.recoverUntil(&b, delimitedExpression, templateBoundaries)
		}
	case "else", "endif", "endfor":
		// These boundaries contain no expression; the sequence closer follows.
	default:
		p.report(UnknownTemplateDirective, keyword.span)
		p.recoverUntil(&b, delimitedExpression, templateBoundaries)
		name = ""
	}

	p.templateSequenceEnd(&b)
	return b.finish(TemplateDirective), name
}

// templateScope is one open if or for directive. Its builder already holds
// the opening header and collects the body until the matching ending.
type templateScope struct {
	builder nodeBuilder
	kind    NodeKind
	// previous is the index of the enclosing scope of the same kind, or -1,
	// which pop restores as lastIf or lastFor.
	previous int
	// sawElse records that an if scope has its else, so a second else is
	// diagnosed rather than attached.
	sawElse bool
}

// templateNesting tracks directive scopes inside one template. Directive
// bodies nest without recursive calls. The latest matching scope indices make
// even repeated unmatched endings linear, rather than searching the entire
// stack each time. Each frame remembers the previous scope of its own kind so
// popping restores those indices in constant time.
type templateNesting struct {
	root    nodeBuilder
	scopes  []templateScope
	lastIf  int
	lastFor int
}

// current returns the builder that receives the next template content: the
// innermost open scope, or the template itself.
func (n *templateNesting) current() *nodeBuilder {
	if len(n.scopes) == 0 {
		return &n.root
	}
	return &n.scopes[len(n.scopes)-1].builder
}

// directive files a parsed header into the scope stack. Openers push a scope.
// else, endif, and endfor attach to the latest scope of their kind after
// closing any unfinished inner scopes; without one they are diagnosed and kept
// in place. Anything else, including an unknown directive, stays in place too.
func (n *templateNesting) directive(header SyntaxNode, name string) {
	p := n.root.parser
	switch name {
	case "if", "for":
		kind, previous := TemplateIf, n.lastIf
		if name == "for" {
			kind, previous = TemplateFor, n.lastFor
			n.lastFor = len(n.scopes)
		} else {
			n.lastIf = len(n.scopes)
		}
		b := p.beginAt(header.Span().Start)
		b.node(header)
		n.scopes = append(n.scopes, templateScope{builder: b, kind: kind, previous: previous})
	case "else", "endif", "endfor":
		target := n.lastIf
		if name == "endfor" {
			target = n.lastFor
		}
		if target < 0 || (name == "else" && n.scopes[target].sawElse) {
			p.report(UnexpectedTemplateDirective, header.Span())
			n.current().node(header)
			return
		}

		// A matching outer boundary must not be swallowed by an unfinished
		// inner scope. Finish those inner nodes before appending this header.
		n.closeMissing(target+1, header.Span())
		n.current().node(header)
		if name == "else" {
			n.scopes[target].sawElse = true
		} else {
			n.pop()
		}
	default:
		n.current().node(header)
	}
}

// closeMissing finishes every scope above remaining, reporting each missing
// ending at boundary: the closer or EOF that ended the template, or the outer
// ending directive that an inner scope failed to close before.
func (n *templateNesting) closeMissing(remaining int, boundary Span) {
	for len(n.scopes) > remaining {
		diagnostic := ExpectedTemplateEndIf
		if n.scopes[len(n.scopes)-1].kind == TemplateFor {
			diagnostic = ExpectedTemplateEndFor
		}
		n.root.parser.report(diagnostic, boundary)
		n.pop()
	}
}

// pop finishes the innermost scope into its node, appends that node to the
// scope below, and restores the latest-of-kind index the scope saved when it
// opened.
func (n *templateNesting) pop() {
	last := n.scopes[len(n.scopes)-1]
	if last.kind == TemplateIf {
		n.lastIf = last.previous
	} else {
		n.lastFor = last.previous
	}

	node := last.builder.finish(last.kind)
	n.scopes = n.scopes[:len(n.scopes)-1]
	n.current().node(node)
}
