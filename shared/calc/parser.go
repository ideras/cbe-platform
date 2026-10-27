package calc

// The parser is a recursive descent over the tokens, building one of the smallest trees that can carry the
// grammar. Implicit multiplication is inserted in the multiplicative loop: a value followed directly by '(' or
// an identifier is a product, while a value followed directly by a number or a '[' is a syntax error. Nesting
// counts every '(' — a grouping or a call — and every '[' of a vector or matrix, and stops at 50.

type nodeKind int

const (
	nodeNumber nodeKind = iota
	nodeIdent
	nodeUnary
	nodeBinary
	nodeFactorial
	nodeCall
	nodeList // a bracket literal: argv are its elements
)

type node struct {
	kind     nodeKind
	text     string  // identifier name
	value    float64 // number literal
	operator tokenKind
	argv     []*node // unary: 1 operand; binary: 2; call: arguments; list: elements
}

// maxNesting is the deepest nesting of parentheses and calls the parser accepts.
const maxNesting = 50

// maxInputLength is the longest input, in Unicode code points.
const maxInputLength = 200

type parser struct {
	tokens []token
	pos    int
	depth  int
}

func parse(tokens []token) (*node, *Error) {
	p := &parser{tokens: tokens}
	expression, err := p.additive()
	if err != nil {
		return nil, err
	}
	if p.peek().kind != tokEOF {
		return nil, &Error{Code: CodeSyntax}
	}
	return expression, nil
}

func (p *parser) peek() token {
	if p.pos < len(p.tokens) {
		return p.tokens[p.pos]
	}
	return token{kind: tokEOF}
}

func (p *parser) next() token {
	t := p.peek()
	if t.kind != tokEOF {
		p.pos++
	}
	return t
}

func (p *parser) additive() (*node, *Error) {
	left, err := p.multiplicative()
	if err != nil {
		return nil, err
	}
	for {
		kind := p.peek().kind
		if kind != tokPlus && kind != tokMinus {
			return left, nil
		}
		p.next()
		right, err := p.multiplicative()
		if err != nil {
			return nil, err
		}
		left = &node{kind: nodeBinary, operator: kind, argv: []*node{left, right}}
	}
}

func (p *parser) multiplicative() (*node, *Error) {
	left, err := p.unary()
	if err != nil {
		return nil, err
	}
	for {
		switch kind := p.peek().kind; kind {
		case tokStar, tokSlash:
			p.next()
			right, err := p.unary()
			if err != nil {
				return nil, err
			}
			left = &node{kind: nodeBinary, operator: kind, argv: []*node{left, right}}
		case tokIdent, tokLParen:
			// Implicit multiplication: 2pi, 3(4+1), 2sin(30). It has the precedence of '*'.
			right, err := p.unary()
			if err != nil {
				return nil, err
			}
			left = &node{kind: nodeBinary, operator: tokStar, argv: []*node{left, right}}
		case tokNumber:
			return nil, &Error{Code: CodeSyntax} // a number directly followed by a number
		default:
			return left, nil
		}
	}
}

func (p *parser) unary() (*node, *Error) {
	switch kind := p.peek().kind; kind {
	case tokMinus, tokPlus:
		p.next()
		operand, err := p.unary()
		if err != nil {
			return nil, err
		}
		return &node{kind: nodeUnary, operator: kind, argv: []*node{operand}}, nil
	default:
		return p.power()
	}
}

func (p *parser) power() (*node, *Error) {
	base, err := p.postfix()
	if err != nil {
		return nil, err
	}
	if p.peek().kind != tokCaret {
		return base, nil
	}
	p.next()
	// The exponent is a unary, so 2^-1 works and 2^3^2 is right-associative.
	exponent, err := p.unary()
	if err != nil {
		return nil, err
	}
	return &node{kind: nodeBinary, operator: tokCaret, argv: []*node{base, exponent}}, nil
}

func (p *parser) postfix() (*node, *Error) {
	primary, err := p.primary()
	if err != nil {
		return nil, err
	}
	if p.peek().kind == tokBang {
		p.next()
		return &node{kind: nodeFactorial, argv: []*node{primary}}, nil
	}
	return primary, nil
}

func (p *parser) primary() (*node, *Error) {
	t := p.next()
	switch t.kind {
	case tokNumber:
		return &node{kind: nodeNumber, value: t.value}, nil
	case tokIdent:
		if p.peek().kind == tokLParen {
			return p.call(t.text)
		}
		return &node{kind: nodeIdent, text: t.text}, nil
	case tokLBracket:
		return p.list()
	case tokLParen:
		if err := p.open(); err != nil {
			return nil, err
		}
		inner, err := p.additive()
		if err != nil {
			return nil, err
		}
		if p.next().kind != tokRParen {
			return nil, &Error{Code: CodeSyntax}
		}
		p.depth--
		return inner, nil
	default:
		return nil, &Error{Code: CodeSyntax}
	}
}

func (p *parser) call(name string) (*node, *Error) {
	p.next() // the '(' after the name
	if err := p.open(); err != nil {
		return nil, err
	}
	var argv []*node
	if p.peek().kind != tokRParen {
		for {
			argument, err := p.additive()
			if err != nil {
				return nil, err
			}
			argv = append(argv, argument)
			if p.peek().kind != tokComma {
				break
			}
			p.next()
		}
	}
	if p.next().kind != tokRParen {
		return nil, &Error{Code: CodeSyntax}
	}
	p.depth--
	return &node{kind: nodeCall, text: name, argv: argv}, nil
}

// list parses a bracket literal after its '[': one or more expressions separated by commas, then ']'. An empty
// [] is SYNTAX; what the elements make (a vector, a matrix, or neither) is decided when they are evaluated.
func (p *parser) list() (*node, *Error) {
	if err := p.open(); err != nil {
		return nil, err
	}
	var argv []*node
	for {
		element, err := p.additive()
		if err != nil {
			return nil, err
		}
		argv = append(argv, element)
		if p.peek().kind != tokComma {
			break
		}
		p.next()
	}
	if p.next().kind != tokRBracket {
		return nil, &Error{Code: CodeSyntax}
	}
	p.depth--
	return &node{kind: nodeList, argv: argv}, nil
}

// open counts an opening parenthesis or bracket, whether a grouping, a call's or a literal's.
func (p *parser) open() *Error {
	p.depth++
	if p.depth > maxNesting {
		return &Error{Code: CodeTooDeep}
	}
	return nil
}
