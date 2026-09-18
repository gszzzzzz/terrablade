package syntax

// templateDirective parses only a %{...} header. Pairing it with a body is a
// separate, iterative step, and malformed headers still retain their keyword so
// a later matching ending can recover the surrounding template structure.
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
		p.operand(&b, 0, delimitedExpression)
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

type templateScope struct {
	builder  nodeBuilder
	kind     NodeKind
	previous int
	sawElse  bool
}

// Directive bodies nest without recursive calls. The latest matching scope
// indices make even repeated unmatched endings linear, rather than searching
// the entire stack each time. Each frame remembers the previous scope of its
// own kind so popping restores those indices in constant time.
type templateNesting struct {
	root    nodeBuilder
	scopes  []templateScope
	lastIf  int
	lastFor int
}

func (n *templateNesting) current() *nodeBuilder {
	if len(n.scopes) == 0 {
		return &n.root
	}
	return &n.scopes[len(n.scopes)-1].builder
}

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
		if target < 0 || name == "else" && n.scopes[target].sawElse {
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
