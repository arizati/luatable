package luatable

import (
	"fmt"
	"math"
)

// Parser parses Lua table constructors. A Parser may be re-used for subsequent
// parsing in order to amortize allocation overhead, but it must not be used
// from concurrent goroutines. Use ParserPool to share parsers safely.
type Parser struct {
	// MaxDepth limits the nesting depth of table constructors. When zero,
	// DefaultMaxDepth is used.
	MaxDepth int

	// AllowReturnPrefix, when true, makes the parser accept an optional
	// "return" statement before the table constructor. This is useful for
	// parsing Lua module files of the form "return { ... }".
	AllowReturnPrefix bool

	// StrictKeywords makes the parser reject Lua reserved words used as bare
	// table keys, matching the reference implementation: "{end = 1}" is invalid
	// Lua and has to be written as '{["end"] = 1}'.
	//
	// The zero value keeps the default lenient behaviour, which accepts such
	// keys so that slightly off-spec data files still load. The encoder is
	// always strict, because its output must be valid Lua.
	StrictKeywords bool

	src string
	lex lexer
}

// Parse parses s, which must contain a single Lua table constructor, and
// returns the generic Go representation of that table.
//
// See the package documentation for the mapping between Lua and Go values.
func (p *Parser) Parse(s string) (any, error) {
	t, err := p.ParseTable(s)
	if err != nil {
		return nil, err
	}
	return t.Interface(), nil
}

// ParseBytes parses b as a Lua table constructor. See Parse.
func (p *Parser) ParseBytes(b []byte) (any, error) {
	return p.Parse(string(b))
}

// ParseTable parses s and returns the rich *Table representation, preserving
// exact key types and insertion order.
func (p *Parser) ParseTable(s string) (*Table, error) {
	p.src = s
	p.lex = lexer{src: s}
	p.lex.next()

	if err := p.lex.err; err != nil {
		return nil, err
	}

	if p.AllowReturnPrefix && p.lex.tok.typ == tokenKeyword && p.lex.tok.text == "return" {
		p.lex.next()
		if err := p.lex.err; err != nil {
			return nil, err
		}
	}

	if p.lex.tok.typ != tokenLBrace {
		return nil, p.errorf(p.lex.tok.offset, "expected table constructor '{', found %s", p.describeToken())
	}

	t, err := p.parseTableConstructor(1)
	if err != nil {
		return nil, err
	}

	// Lua allows empty statements after a chunk; tolerate trailing ';'.
	for p.lex.tok.typ == tokenSemicolon {
		p.lex.next()
		if err := p.lex.err; err != nil {
			return nil, err
		}
	}

	if p.lex.tok.typ != tokenEOF {
		return nil, p.errorf(p.lex.tok.offset, "unexpected %s after table constructor", p.describeToken())
	}

	return t, nil
}

// ParseTableBytes parses b as a Lua table constructor. See ParseTable.
func (p *Parser) ParseTableBytes(b []byte) (*Table, error) {
	return p.ParseTable(string(b))
}

func (p *Parser) maxDepth() int {
	if p.MaxDepth > 0 {
		return p.MaxDepth
	}
	return DefaultMaxDepth
}

// parseTableConstructor parses "{ [fieldlist] }". The current token must be
// the opening brace.
func (p *Parser) parseTableConstructor(depth int) (*Table, error) {
	if depth > p.maxDepth() {
		return nil, p.errorf(p.lex.tok.offset, "table nesting depth exceeds the maximum of %d", p.maxDepth())
	}

	openTok := p.lex.tok
	p.lex.next()
	if err := p.lex.err; err != nil {
		return nil, err
	}

	tb := newTableBuilder()

	if p.lex.tok.typ == tokenRBrace {
		p.lex.next()
		if err := p.lex.err; err != nil {
			return nil, err
		}
		return tb.build(), nil
	}

	for {
		if p.lex.tok.typ == tokenEOF {
			return nil, p.errorf(p.lex.tok.offset,
				"unexpected end of input, expected '}' to close the table constructor opened at offset %d", openTok.offset)
		}

		if err := p.parseField(tb, depth); err != nil {
			return nil, err
		}

		switch p.lex.tok.typ {
		case tokenComma, tokenSemicolon:
			p.lex.next()
			if err := p.lex.err; err != nil {
				return nil, err
			}
			// A trailing separator before '}' is allowed.
			if p.lex.tok.typ == tokenRBrace {
				p.lex.next()
				if err := p.lex.err; err != nil {
					return nil, err
				}
				return tb.build(), nil
			}
		case tokenRBrace:
			p.lex.next()
			if err := p.lex.err; err != nil {
				return nil, err
			}
			return tb.build(), nil
		case tokenEOF:
			return nil, p.errorf(p.lex.tok.offset,
				"unexpected end of input, expected '}' to close the table constructor opened at offset %d", openTok.offset)
		default:
			return nil, p.errorf(p.lex.tok.offset, "expected ',' or '}' after table field, found %s", p.describeToken())
		}
	}
}

// parseField parses a single field of a table constructor and stores it in tb.
func (p *Parser) parseField(tb *tableBuilder, depth int) error {
	tok := p.lex.tok

	switch tok.typ {
	case tokenName:
		// Either an identifier key ("name = value") or an unsupported
		// expression (a bare identifier is never a literal value).
		p.lex.next()
		if err := p.lex.err; err != nil {
			return err
		}
		if p.lex.tok.typ != tokenAssign {
			return p.errorf(tok.offset,
				"unsupported expression %q; expected a literal, a nested table or an 'name = value' field", tok.text)
		}
		if p.StrictKeywords && reservedWords[tok.text] {
			return p.errorf(tok.offset,
				"reserved word %q cannot be used as a table key; write [%q] instead", tok.text, tok.text)
		}
		p.lex.next()
		if err := p.lex.err; err != nil {
			return err
		}
		value, err := p.parseValue(depth)
		if err != nil {
			return err
		}
		tb.set(tok.text, value)
		return nil

	case tokenLBracket:
		openTok := tok
		p.lex.next()
		if err := p.lex.err; err != nil {
			return err
		}

		key, err := p.parseValue(depth)
		if err != nil {
			return err
		}

		if p.lex.tok.typ != tokenRBracket {
			return p.errorf(p.lex.tok.offset, "expected ']' to close the table key, found %s", p.describeToken())
		}
		p.lex.next()
		if err := p.lex.err; err != nil {
			return err
		}

		if p.lex.tok.typ != tokenAssign {
			return p.errorf(p.lex.tok.offset, "expected '=' after the table key, found %s", p.describeToken())
		}
		p.lex.next()
		if err := p.lex.err; err != nil {
			return err
		}

		value, err := p.parseValue(depth)
		if err != nil {
			return err
		}

		if key == nil {
			return p.errorf(openTok.offset, "table index is nil")
		}
		// Lua rejects a NaN key as well ("table index is NaN") and accepts an
		// infinite one. This package rejects both: a NaN key could never be
		// looked up again in the resulting Table, and an infinity has no
		// literal the encoder could write back.
		if f, isFloat := key.(float64); isFloat && (math.IsNaN(f) || math.IsInf(f, 0)) {
			return p.errorf(openTok.offset, "table key is NaN or infinite")
		}
		normalized, ok := normalizeKey(key)
		if !ok {
			return p.errorf(openTok.offset, "unsupported table key type %T", key)
		}
		tb.t.set(normalized, value)
		return nil

	default:
		// Positional field: part of the array section.
		value, err := p.parseValue(depth)
		if err != nil {
			return err
		}
		tb.append(value)
		return nil
	}
}

// parseValue parses a single literal value, a nested table constructor, a
// unary minus or a parenthesized expression.
//
// depth bounds the total recursion, covering nested tables as well as chains
// of unary minus and parentheses, so that hostile input cannot exhaust the
// goroutine stack.
func (p *Parser) parseValue(depth int) (any, error) {
	if depth > p.maxDepth() {
		return nil, p.errorf(p.lex.tok.offset, "expression nesting depth exceeds the maximum of %d", p.maxDepth())
	}

	tok := p.lex.tok

	switch tok.typ {
	case tokenLBrace:
		return p.parseTableConstructor(depth + 1)

	case tokenNumber:
		p.lex.next()
		if err := p.lex.err; err != nil {
			return nil, err
		}
		n, err := parseLuaNumber(tok.text)
		if err != nil {
			return nil, p.errorf(tok.offset, "%s", err)
		}
		return n, nil

	case tokenString:
		p.lex.next()
		if err := p.lex.err; err != nil {
			return nil, err
		}
		s, errOffset, err := decodeStringLiteral(tok.text)
		if err != nil {
			return nil, p.errorf(tok.offset+errOffset, "%s", err)
		}
		return s, nil

	case tokenKeyword:
		p.lex.next()
		if err := p.lex.err; err != nil {
			return nil, err
		}
		switch tok.text {
		case "nil":
			return nil, nil
		case "true":
			return true, nil
		case "false":
			return false, nil
		case "return":
			return nil, p.errorf(tok.offset, "unexpected 'return' inside a table constructor")
		default:
			return nil, p.errorf(tok.offset, "unexpected keyword %q", tok.text)
		}

	case tokenMinus:
		p.lex.next()
		if err := p.lex.err; err != nil {
			return nil, err
		}
		value, err := p.parseValue(depth + 1)
		if err != nil {
			return nil, err
		}
		number, ok := negateNumber(value)
		if !ok {
			return nil, p.errorf(tok.offset, "unary '-' requires a number operand")
		}
		return number, nil

	case tokenLParen:
		p.lex.next()
		if err := p.lex.err; err != nil {
			return nil, err
		}
		value, err := p.parseValue(depth + 1)
		if err != nil {
			return nil, err
		}
		if p.lex.tok.typ != tokenRParen {
			return nil, p.errorf(p.lex.tok.offset, "expected ')', found %s", p.describeToken())
		}
		p.lex.next()
		if err := p.lex.err; err != nil {
			return nil, err
		}
		return value, nil

	case tokenName:
		// A bare identifier is never a literal value. Report the error
		// without consuming further tokens so that the position points at
		// the offending identifier.
		return nil, p.errorf(tok.offset,
			"unsupported expression %q; only literals, nested tables and unary minus are supported", tok.text)

	case tokenEOF:
		return nil, p.errorf(tok.offset, "unexpected end of input, expected a value")

	default:
		return nil, p.errorf(tok.offset, "unexpected %s, expected a value", p.describeToken())
	}
}

// negateNumber returns the arithmetic negation of a numeric value.
func negateNumber(v any) (any, bool) {
	switch n := v.(type) {
	case int64:
		if n == math.MinInt64 {
			return -float64(n), true
		}
		return -n, true
	case float64:
		return -n, true
	default:
		return nil, false
	}
}

func (p *Parser) describeToken() string {
	t := p.lex.tok
	switch t.typ {
	case tokenName:
		return fmt.Sprintf("identifier %q", t.text)
	case tokenKeyword:
		return fmt.Sprintf("%q", t.text)
	case tokenNumber:
		return fmt.Sprintf("number literal %q", t.text)
	default:
		return t.typ.String()
	}
}

func (p *Parser) errorf(offset int, format string, args ...any) error {
	return newSyntaxError(p.src, offset, format, args...)
}
