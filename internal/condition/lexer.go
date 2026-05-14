package condition

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// tokenKind enumerates lexer token classes.
type tokenKind int

const (
	tkEOF tokenKind = iota
	tkIdent
	tkString
	tkNumber
	tkBool
	tkLBrack
	tkRBrack
	tkLBrace
	tkRBrace
	tkLParen
	tkRParen
	tkComma
	tkColon
	// Operator keywords / symbols
	tkEq          // =
	tkNeq         // !=
	tkLt          // <
	tkLte         // <=
	tkGt          // >
	tkGte         // >=
	tkMatches     // ~ or MATCHES
	tkExistsSym   // ?
	tkContains    // CONTAINS
	tkIn          // IN
	tkAnd         // & or AND
	tkOr          // | or OR
	tkNot         // ! or NOT
	tkExistsKW    // EXISTS
)

// token is a single lexer output.
type token struct {
	kind tokenKind
	// text is the original source slice for idents and the decoded body for strings;
	// for numbers, it is the original source slice (parsed lazily).
	text string
	// num is the numeric value when kind == tkNumber. isFloat reports float vs int.
	num     float64
	isInt   bool
	intVal  int64
	isFloat bool
	// boolVal when kind == tkBool.
	boolVal bool
	// pos is the byte offset of the token start in the source.
	pos int
}

// lex tokenises s. Whitespace is significant only as a separator between
// implicit-AND terms; that decision is made by the parser, not the lexer.
func lex(s string) ([]token, error) {
	l := &lexer{src: s}
	for {
		t, err := l.next()
		if err != nil {
			return nil, err
		}
		l.out = append(l.out, t)
		if t.kind == tkEOF {
			break
		}
	}
	return l.out, nil
}

type lexer struct {
	src string
	pos int
	out []token
}

func (l *lexer) errf(pos int, tok, format string, args ...any) error {
	return &ParseError{Pos: pos, Token: tok, Message: fmt.Sprintf(format, args...)}
}

func (l *lexer) peek() (rune, int) {
	if l.pos >= len(l.src) {
		return 0, 0
	}
	r, w := utf8.DecodeRuneInString(l.src[l.pos:])
	return r, w
}

func (l *lexer) advance() rune {
	r, w := l.peek()
	l.pos += w
	return r
}

func (l *lexer) skipSpace() {
	for l.pos < len(l.src) {
		r, w := l.peek()
		if !unicode.IsSpace(r) {
			return
		}
		l.pos += w
	}
}

func (l *lexer) next() (token, error) {
	l.skipSpace()
	if l.pos >= len(l.src) {
		return token{kind: tkEOF, pos: l.pos}, nil
	}
	start := l.pos
	r, _ := l.peek()
	switch r {
	case '(':
		l.advance()
		return token{kind: tkLParen, pos: start}, nil
	case ')':
		l.advance()
		return token{kind: tkRParen, pos: start}, nil
	case '[':
		l.advance()
		return token{kind: tkLBrack, pos: start}, nil
	case ']':
		l.advance()
		return token{kind: tkRBrack, pos: start}, nil
	case '{':
		l.advance()
		return token{kind: tkLBrace, pos: start}, nil
	case '}':
		l.advance()
		return token{kind: tkRBrace, pos: start}, nil
	case ',':
		l.advance()
		return token{kind: tkComma, pos: start}, nil
	case ':':
		l.advance()
		return token{kind: tkColon, pos: start}, nil
	case '&':
		l.advance()
		return token{kind: tkAnd, pos: start}, nil
	case '|':
		l.advance()
		return token{kind: tkOr, pos: start}, nil
	case '~':
		l.advance()
		return token{kind: tkMatches, pos: start}, nil
	case '?':
		l.advance()
		return token{kind: tkExistsSym, pos: start}, nil
	case '=':
		l.advance()
		return token{kind: tkEq, pos: start}, nil
	case '!':
		l.advance()
		if l.pos < len(l.src) && l.src[l.pos] == '=' {
			l.pos++
			return token{kind: tkNeq, pos: start}, nil
		}
		return token{kind: tkNot, pos: start}, nil
	case '<':
		l.advance()
		if l.pos < len(l.src) && l.src[l.pos] == '=' {
			l.pos++
			return token{kind: tkLte, pos: start}, nil
		}
		return token{kind: tkLt, pos: start}, nil
	case '>':
		l.advance()
		if l.pos < len(l.src) && l.src[l.pos] == '=' {
			l.pos++
			return token{kind: tkGte, pos: start}, nil
		}
		return token{kind: tkGt, pos: start}, nil
	case '"', '\'':
		return l.readString(r)
	}
	if r == '-' || r == '+' || (r >= '0' && r <= '9') || r == '.' {
		if tok, ok, err := l.tryReadNumber(); err != nil {
			return token{}, err
		} else if ok {
			return tok, nil
		}
	}
	if isIdentStart(r) {
		return l.readIdent(), nil
	}
	return token{}, l.errf(start, string(r), "unexpected character")
}

func (l *lexer) readString(quote rune) (token, error) {
	start := l.pos
	l.advance() // consume opening quote
	var b strings.Builder
	for {
		if l.pos >= len(l.src) {
			return token{}, l.errf(start, "", "unterminated string literal")
		}
		r := l.advance()
		if r == '\\' {
			if l.pos >= len(l.src) {
				return token{}, l.errf(start, "", "trailing backslash in string")
			}
			esc := l.advance()
			switch esc {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'r':
				b.WriteByte('\r')
			case '\\':
				b.WriteByte('\\')
			case '"':
				b.WriteByte('"')
			case '\'':
				b.WriteByte('\'')
			case '/':
				b.WriteByte('/')
			case '0':
				b.WriteByte(0)
			default:
				// Preserve unknown escapes verbatim (Python pyparsing's escChar
				// only consumes the next char; we mirror that loosely).
				b.WriteRune(esc)
			}
			continue
		}
		if r == quote {
			return token{kind: tkString, text: b.String(), pos: start}, nil
		}
		b.WriteRune(r)
	}
}

func (l *lexer) tryReadNumber() (token, bool, error) {
	start := l.pos
	saved := l.pos
	// Allow leading sign — but the parser will treat '-' as a prefix only when
	// the surrounding context permits a literal here.
	if r, _ := l.peek(); r == '-' || r == '+' {
		l.advance()
	}
	hadDigit := false
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		if c >= '0' && c <= '9' {
			hadDigit = true
			l.pos++
		} else {
			break
		}
	}
	hasDot := false
	if l.pos < len(l.src) && l.src[l.pos] == '.' {
		// Only treat '.' as fractional if followed by digit; otherwise rollback.
		if l.pos+1 < len(l.src) && l.src[l.pos+1] >= '0' && l.src[l.pos+1] <= '9' {
			hasDot = true
			l.pos++
			for l.pos < len(l.src) {
				c := l.src[l.pos]
				if c >= '0' && c <= '9' {
					hadDigit = true
					l.pos++
				} else {
					break
				}
			}
		}
	}
	hasExp := false
	if l.pos < len(l.src) && (l.src[l.pos] == 'e' || l.src[l.pos] == 'E') {
		expStart := l.pos
		l.pos++
		if l.pos < len(l.src) && (l.src[l.pos] == '+' || l.src[l.pos] == '-') {
			l.pos++
		}
		expHasDigit := false
		for l.pos < len(l.src) {
			c := l.src[l.pos]
			if c >= '0' && c <= '9' {
				expHasDigit = true
				l.pos++
			} else {
				break
			}
		}
		if !expHasDigit {
			// Not actually a number; roll back exponent and stop here.
			l.pos = expStart
		} else {
			hasExp = true
		}
	}
	if !hadDigit {
		l.pos = saved
		return token{}, false, nil
	}
	// If the next char would make this an ident continuation (letter/_),
	// roll back — it's an identifier, not a number.
	if l.pos < len(l.src) {
		c, _ := utf8.DecodeRuneInString(l.src[l.pos:])
		if isIdentPart(c) && c != '-' && c != '.' {
			l.pos = saved
			return token{}, false, nil
		}
	}
	text := l.src[start:l.pos]
	tok := token{kind: tkNumber, text: text, pos: start}
	if hasDot || hasExp {
		f, err := parseFloat(text)
		if err != nil {
			return token{}, false, l.errf(start, text, "invalid number")
		}
		tok.num = f
		tok.isFloat = true
	} else {
		i, err := parseInt(text)
		if err != nil {
			// fall back to float
			f, ferr := parseFloat(text)
			if ferr != nil {
				return token{}, false, l.errf(start, text, "invalid number")
			}
			tok.num = f
			tok.isFloat = true
		} else {
			tok.intVal = i
			tok.isInt = true
		}
	}
	return tok, true, nil
}

func (l *lexer) readIdent() token {
	start := l.pos
	for l.pos < len(l.src) {
		r, w := utf8.DecodeRuneInString(l.src[l.pos:])
		if !isIdentPart(r) {
			break
		}
		l.pos += w
	}
	text := l.src[start:l.pos]
	upper := strings.ToUpper(text)
	switch upper {
	case "AND":
		return token{kind: tkAnd, text: text, pos: start}
	case "OR":
		return token{kind: tkOr, text: text, pos: start}
	case "NOT":
		return token{kind: tkNot, text: text, pos: start}
	case "MATCHES":
		return token{kind: tkMatches, text: text, pos: start}
	case "EXISTS":
		return token{kind: tkExistsKW, text: text, pos: start}
	case "CONTAINS":
		return token{kind: tkContains, text: text, pos: start}
	case "IN":
		return token{kind: tkIn, text: text, pos: start}
	case "TRUE":
		return token{kind: tkBool, text: text, boolVal: true, pos: start}
	case "FALSE":
		return token{kind: tkBool, text: text, boolVal: false, pos: start}
	}
	return token{kind: tkIdent, text: text, pos: start}
}

func isIdentStart(r rune) bool {
	return r == '_' || r == '.' || r == '-' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

// isIdentPart matches Python's `valid_word = r'[a-zA-Z0-9_.-]+'` and accepts
// '.' and '-' inside the identifier.
func isIdentPart(r rune) bool {
	return r == '_' || r == '.' || r == '-' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

func parseInt(s string) (int64, error) {
	var n int64
	neg := false
	i := 0
	if i < len(s) && (s[i] == '-' || s[i] == '+') {
		neg = s[i] == '-'
		i++
	}
	if i == len(s) {
		return 0, fmt.Errorf("empty int")
	}
	for ; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("bad int")
		}
		n = n*10 + int64(c-'0')
	}
	if neg {
		n = -n
	}
	return n, nil
}

func parseFloat(s string) (float64, error) {
	return strconv.ParseFloat(s, 64)
}
