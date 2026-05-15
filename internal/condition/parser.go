package condition

import "fmt"

// Parse converts a query DSL string into a Cond AST. Mirrors the pyparsing
// grammar in src/snooze/utils/parser.py.
//
// Precedence (highest to lowest): NOT > OR > AND. Whitespace between two
// terms is an implicit AND.
func Parse(s string) (Cond, error) {
	toks, err := lex(s)
	if err != nil {
		return Cond{}, err
	}
	p := &parser{toks: toks}
	c, err := p.parseExpr()
	if err != nil {
		return Cond{}, err
	}
	if p.peek().kind != tkEOF {
		t := p.peek()
		return Cond{}, &ParseError{Pos: t.pos, Token: t.text, Message: "unexpected trailing input"}
	}
	return c, nil
}

type parser struct {
	toks []token
	pos  int
}

func (p *parser) peek() token { return p.toks[p.pos] }

func (p *parser) advance() token {
	t := p.toks[p.pos]
	if t.kind != tkEOF {
		p.pos++
	}
	return t
}

func (p *parser) errf(t token, format string, args ...any) error {
	return &ParseError{Pos: t.pos, Token: t.text, Message: fmt.Sprintf(format, args...)}
}

// Grammar (recursive-descent, lowest precedence first):
//
//	expr      := andExpr
//	andExpr   := orExpr (AND? orExpr)*
//	orExpr    := notExpr (OR notExpr)*
//	notExpr   := NOT notExpr | term
//	term      := '(' expr ')' | <fieldOpValue> | <fieldExists> | <valueIn> | <searchLiteral>

func (p *parser) parseExpr() (Cond, error) {
	return p.parseAnd()
}

func (p *parser) parseAnd() (Cond, error) {
	left, err := p.parseOr()
	if err != nil {
		return Cond{}, err
	}
	for {
		switch p.peek().kind {
		case tkAnd:
			p.advance()
			r, err := p.parseOr()
			if err != nil {
				return Cond{}, err
			}
			left = mergeAnd(left, r)
		case tkRParen, tkRBrack, tkRBrace, tkComma, tkEOF:
			return left, nil
		default:
			// Implicit AND: peek a token that could start a new term.
			if isTermStart(p.peek()) {
				r, err := p.parseOr()
				if err != nil {
					return Cond{}, err
				}
				left = mergeAnd(left, r)
				continue
			}
			return left, nil
		}
	}
}

func (p *parser) parseOr() (Cond, error) {
	left, err := p.parseNot()
	if err != nil {
		return Cond{}, err
	}
	for p.peek().kind == tkOr {
		p.advance()
		r, err := p.parseNot()
		if err != nil {
			return Cond{}, err
		}
		left = mergeOr(left, r)
	}
	return left, nil
}

func (p *parser) parseNot() (Cond, error) {
	if p.peek().kind == tkNot {
		p.advance()
		inner, err := p.parseNot()
		if err != nil {
			return Cond{}, err
		}
		return Cond{Op: OpNot, Children: []Cond{inner}}, nil
	}
	return p.parseTerm()
}

// mergeAnd / mergeOr keep the AST shape Python's pyparsing produces for
// left-associative binary ops: a, b, c => (a AND (b AND c)) for AND/OR.
// Looking at test_complex_query, AND is right-associative in the output:
//
//	a AND b AND c => ['AND', a, ['AND', b, c]].
//
// So we model AND/OR as nested binary nodes, matching the Python output.
func mergeAnd(left, right Cond) Cond {
	if left.Op == OpAnd && len(left.Children) == 2 {
		return Cond{Op: OpAnd, Children: []Cond{left.Children[0], mergeAnd(left.Children[1], right)}}
	}
	return Cond{Op: OpAnd, Children: []Cond{left, right}}
}

func mergeOr(left, right Cond) Cond {
	if left.Op == OpOr && len(left.Children) == 2 {
		return Cond{Op: OpOr, Children: []Cond{left.Children[0], mergeOr(left.Children[1], right)}}
	}
	return Cond{Op: OpOr, Children: []Cond{left, right}}
}

func isTermStart(t token) bool {
	switch t.kind {
	case tkIdent, tkString, tkNumber, tkBool, tkLParen, tkLBrack, tkLBrace, tkNot:
		return true
	}
	return false
}

func (p *parser) parseTerm() (Cond, error) {
	if p.peek().kind == tkLParen {
		p.advance()
		c, err := p.parseExpr()
		if err != nil {
			return Cond{}, err
		}
		if p.peek().kind != tkRParen {
			return Cond{}, p.errf(p.peek(), "expected ')'")
		}
		p.advance()
		return c, nil
	}

	// Try the various forms of `term`. We do not commit to a form until we've
	// seen the operator following the first literal/identifier.
	first, _, err := p.parseFirstAtom()
	if err != nil {
		return Cond{}, err
	}

	t := p.peek()
	switch t.kind {
	case tkEq:
		p.advance()
		v, err := p.parseLiteral()
		if err != nil {
			return Cond{}, err
		}
		fname, ok := asFieldName(first)
		if !ok {
			return Cond{}, p.errf(t, "left of '=' must be a field name")
		}
		return Cond{Op: OpEq, Field: fname, Value: v}, nil
	case tkNeq:
		p.advance()
		v, err := p.parseLiteral()
		if err != nil {
			return Cond{}, err
		}
		fname, ok := asFieldName(first)
		if !ok {
			return Cond{}, p.errf(t, "left of '!=' must be a field name")
		}
		return Cond{Op: OpNeq, Field: fname, Value: v}, nil
	case tkLt:
		p.advance()
		v, err := p.parseLiteral()
		if err != nil {
			return Cond{}, err
		}
		fname, ok := asFieldName(first)
		if !ok {
			return Cond{}, p.errf(t, "left of '<' must be a field name")
		}
		return Cond{Op: OpLt, Field: fname, Value: v}, nil
	case tkLte:
		p.advance()
		v, err := p.parseLiteral()
		if err != nil {
			return Cond{}, err
		}
		fname, ok := asFieldName(first)
		if !ok {
			return Cond{}, p.errf(t, "left of '<=' must be a field name")
		}
		return Cond{Op: OpLte, Field: fname, Value: v}, nil
	case tkGt:
		p.advance()
		v, err := p.parseLiteral()
		if err != nil {
			return Cond{}, err
		}
		fname, ok := asFieldName(first)
		if !ok {
			return Cond{}, p.errf(t, "left of '>' must be a field name")
		}
		return Cond{Op: OpGt, Field: fname, Value: v}, nil
	case tkGte:
		p.advance()
		v, err := p.parseLiteral()
		if err != nil {
			return Cond{}, err
		}
		fname, ok := asFieldName(first)
		if !ok {
			return Cond{}, p.errf(t, "left of '>=' must be a field name")
		}
		return Cond{Op: OpGte, Field: fname, Value: v}, nil
	case tkMatches:
		p.advance()
		v, err := p.parseLiteral()
		if err != nil {
			return Cond{}, err
		}
		fname, ok := asFieldName(first)
		if !ok {
			return Cond{}, p.errf(t, "left of MATCHES must be a field name")
		}
		return Cond{Op: OpMatches, Field: fname, Value: v}, nil
	case tkContains:
		p.advance()
		v, err := p.parseLiteral()
		if err != nil {
			return Cond{}, err
		}
		fname, ok := asFieldName(first)
		if !ok {
			return Cond{}, p.errf(t, "left of CONTAINS must be a field name")
		}
		return Cond{Op: OpContains, Field: fname, Value: v}, nil
	case tkExistsKW, tkExistsSym:
		p.advance()
		fname, ok := asFieldName(first)
		if !ok {
			return Cond{}, p.errf(t, "left of EXISTS must be a field name")
		}
		return Cond{Op: OpExists, Field: fname}, nil
	case tkIn:
		p.advance()
		// Right-hand side is a field name (identifier or string).
		rt := p.peek()
		if rt.kind != tkIdent && rt.kind != tkString {
			return Cond{}, p.errf(rt, "right of IN must be a field name")
		}
		p.advance()
		return Cond{Op: OpIn, Field: rt.text, Value: first}, nil
	}

	// Otherwise: SEARCH form.
	return Cond{Op: OpSearch, Value: first}, nil
}

// parseFirstAtom reads either a literal or a field-name reference. The returned
// "wasFieldName" flag is informational; the parser only uses the actual value
// shape (string/list/etc) to decide.
func (p *parser) parseFirstAtom() (any, bool, error) {
	t := p.peek()
	switch t.kind {
	case tkIdent:
		p.advance()
		return t.text, true, nil
	case tkString:
		p.advance()
		return t.text, false, nil
	}
	v, err := p.parseLiteral()
	if err != nil {
		return nil, false, err
	}
	return v, false, nil
}

func (p *parser) parseLiteral() (any, error) {
	t := p.peek()
	switch t.kind {
	case tkString:
		p.advance()
		return t.text, nil
	case tkNumber:
		p.advance()
		if t.isInt {
			return t.intVal, nil
		}
		return t.num, nil
	case tkBool:
		p.advance()
		return t.boolVal, nil
	case tkIdent:
		p.advance()
		return t.text, nil
	case tkLBrack:
		return p.parseArray()
	case tkLBrace:
		return p.parseObject()
	}
	return nil, p.errf(t, "expected literal")
}

func (p *parser) parseArray() (any, error) {
	if p.peek().kind != tkLBrack {
		return nil, p.errf(p.peek(), "expected '['")
	}
	p.advance()
	out := []any{}
	if p.peek().kind == tkRBrack {
		p.advance()
		return out, nil
	}
	for {
		v, err := p.parseLiteral()
		if err != nil {
			return nil, err
		}
		out = append(out, v)
		if p.peek().kind == tkComma {
			p.advance()
			continue
		}
		break
	}
	if p.peek().kind != tkRBrack {
		return nil, p.errf(p.peek(), "expected ']' or ','")
	}
	p.advance()
	return out, nil
}

func (p *parser) parseObject() (any, error) {
	if p.peek().kind != tkLBrace {
		return nil, p.errf(p.peek(), "expected '{'")
	}
	p.advance()
	out := map[string]any{}
	if p.peek().kind == tkRBrace {
		p.advance()
		return out, nil
	}
	for {
		kt := p.peek()
		var key string
		switch kt.kind {
		case tkString, tkIdent:
			key = kt.text
			p.advance()
		default:
			return nil, p.errf(kt, "expected map key")
		}
		if p.peek().kind != tkColon {
			return nil, p.errf(p.peek(), "expected ':'")
		}
		p.advance()
		v, err := p.parseLiteral()
		if err != nil {
			return nil, err
		}
		out[key] = v
		if p.peek().kind == tkComma {
			p.advance()
			continue
		}
		break
	}
	if p.peek().kind != tkRBrace {
		return nil, p.errf(p.peek(), "expected '}' or ','")
	}
	p.advance()
	return out, nil
}

// asFieldName extracts a field name from an atom that could be a string or
// identifier. Returns false if the atom is not stringy.
func asFieldName(v any) (string, bool) {
	s, ok := v.(string)
	return s, ok
}
