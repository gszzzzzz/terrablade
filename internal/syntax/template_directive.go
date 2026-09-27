package syntax

// templateDirective parses a %{...} header and returns it with its keyword, or
// "" if the keyword is missing or unknown. A malformed header keeps its
// keyword, so that it can still pair with its ending.
func (p *parser) templateDirective() (Node, string) {
	b := p.begin()
	p.templateSequenceOpen(&b)
	keyword := p.tokens[p.look(newlineTransparent)]
	if keyword.kind != Identifier {
		p.report(ExpectedTemplateDirective, keyword.span)
		p.recoverUntil(&b, newlineTransparent, templateBoundaries)
		p.templateSequenceEnd(&b)
		return b.finish(TemplateDirective), ""
	}

	p.consumeLookahead(&b, newlineTransparent)
	name := p.source[keyword.span.Start:keyword.span.End]
	switch name {
	case "if":
		p.operand(&b, lowestPower, newlineTransparent)
	case "for":
		if !p.forIntroduction(&b) {
			p.recoverUntil(&b, newlineTransparent, templateBoundaries)
		}
	case "else", "endif", "endfor":
	default:
		p.report(UnknownTemplateDirective, keyword.span)
		p.recoverUntil(&b, newlineTransparent, templateBoundaries)
		name = ""
	}

	p.templateSequenceEnd(&b)
	return b.finish(TemplateDirective), name
}

// templateScope is one open if or for directive.
type templateScope struct {
	builder  nodeBuilder
	kind     NodeKind
	previous int // index of the enclosing scope of the same kind, or -1
	sawElse  bool
}

// templateNesting tracks the open directive scopes of one template. lastIf
// and lastFor index the innermost scope of each kind, so an ending finds its
// scope in constant time.
type templateNesting struct {
	root    nodeBuilder
	scopes  []templateScope
	lastIf  int
	lastFor int
}

// current returns the builder for the innermost open scope, or the template.
func (n *templateNesting) current() *nodeBuilder {
	if len(n.scopes) == 0 {
		return &n.root
	}
	return &n.scopes[len(n.scopes)-1].builder
}

// directive adds a header to the scope stack. "if" and "for" open a scope;
// "else", "endif", and "endfor" join the innermost scope of their kind, first
// closing any scopes inside it.
func (n *templateNesting) directive(header Node, name string) {
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

// closeMissing pops scopes until remaining are left, reporting each missing
// ending at boundary.
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

// pop finishes the innermost scope and appends it to the one below.
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
