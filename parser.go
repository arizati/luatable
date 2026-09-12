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

	// Lenient keeps parsing when a field cannot be decoded as a literal: the
	// value is consumed and recorded as a Skipped instead of being rejected.
	// A key that is not a literal is consumed as well, and its field is
	// dropped, because a Table key can only be a literal.
	//
	// The zero value keeps the strict behaviour, which accepts only literals,
	// nested tables, unary minus and parenthesized literals. Lenient is a
	// recovery mode for data files that mix literals with code; it is not a
	// validation mode, and it does not relax structural errors such as an
	// unbalanced constructor or a missing value.
	Lenient bool

	src string
	lex lexer

	// newSink builds the sink that collects one table constructor. Parse
	// selects the generic sink and ParseTable the rich one; the recursion is
	// the same for both.
	newSink func() tableSink

	// sink is the sink of the table constructor that is currently open.
	// parseTableConstructor saves and restores it around each table, so a
	// nested table gets its own sink.
	//
	// It is a field rather than a parameter because parseField, which stores
	// every field, cannot carry it as an interface parameter without that
	// parameter escaping to the heap once per table.
	sink tableSink

	// next is the array index that the next positional field of the open table
	// constructor receives. It counts the positional fields of that
	// constructor only, as Lua does, and parseTableConstructor saves and
	// restores it along with the sink.
	next int64
}

// Parse parses s, which must contain a single Lua table constructor, and
// returns the generic Go representation of that table.
//
// Parse builds that representation directly, without constructing a *Table
// first; use ParseTable when exact key types or entry order are needed.
//
// See the package documentation for the mapping between Lua and Go values.
func (p *Parser) Parse(s string) (any, error) {
	p.newSink = newGenericSink
	return p.parse(s)
}

// ParseBytes parses b as a Lua table constructor. See Parse.
func (p *Parser) ParseBytes(b []byte) (any, error) {
	return p.Parse(string(b))
}

// ParseTable parses s and returns the rich *Table representation, preserving
// exact key types and insertion order.
func (p *Parser) ParseTable(s string) (*Table, error) {
	p.newSink = newTableSink

	value, err := p.parse(s)
	if err != nil {
		return nil, err
	}

	// The rich sink stores a *Table, so the assertion cannot fail.
	return value.(*Table), nil
}

// parse parses s with the sink that p.newSink selects and returns its result:
// a *Table for the rich sink, a []any or map[string]any for the generic one.
func (p *Parser) parse(s string) (any, error) {
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
// the opening brace. It returns what the selected sink produces for the table:
// a *Table, or the generic representation.
func (p *Parser) parseTableConstructor(depth int) (any, error) {
	if depth > p.maxDepth() {
		return nil, p.errorf(p.lex.tok.offset, "table nesting depth exceeds the maximum of %d", p.maxDepth())
	}

	// Suspend the sink and the field index of the enclosing table and restore
	// them on the way out. At the top level both are zero, so a finished parse
	// leaves the parser holding no reference to its result.
	outerSink, outerNext := p.sink, p.next
	p.sink, p.next = p.newSink(), 0

	value, err := p.parseTableBody(depth, p.lex.tok)

	p.sink, p.next = outerSink, outerNext
	return value, err
}

// storePositional adds a positional field to the open sink, under the next
// array index.
func (p *Parser) storePositional(value any) {
	p.next++
	p.sink.positional(p.next, value)
}

// parseTableBody parses the fields of the table constructor that p.sink
// collects, up to and including its closing '}'. openTok is its opening brace.
// It is separate from parseTableConstructor so that the sink has a single
// save/restore point.
func (p *Parser) parseTableBody(depth int, openTok token) (any, error) {
	p.lex.next()
	if err := p.lex.err; err != nil {
		return nil, err
	}

	if p.lex.tok.typ == tokenRBrace {
		p.lex.next()
		if err := p.lex.err; err != nil {
			return nil, err
		}
		return p.sink.result(), nil
	}

	for {
		if p.lex.tok.typ == tokenEOF {
			return nil, p.errorf(p.lex.tok.offset,
				"unexpected end of input, expected '}' to close the table constructor opened at offset %d", openTok.offset)
		}

		if err := p.parseField(depth); err != nil {
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
				return p.sink.result(), nil
			}
		case tokenRBrace:
			p.lex.next()
			if err := p.lex.err; err != nil {
				return nil, err
			}
			return p.sink.result(), nil
		case tokenEOF:
			return nil, p.errorf(p.lex.tok.offset,
				"unexpected end of input, expected '}' to close the table constructor opened at offset %d", openTok.offset)
		default:
			// An operator here continues the value of the field, which is an
			// expression the strict grammar does not accept.
			if p.lex.tok.typ == tokenOperator {
				return nil, p.errorf(p.lex.tok.offset,
					"unsupported operator %q; only literals, nested tables and unary minus are supported", p.lex.tok.text)
			}
			return nil, p.errorf(p.lex.tok.offset, "expected ',' or '}' after table field, found %s", p.describeToken())
		}
	}
}

// parseField parses a single field of a table constructor and stores it in the
// open sink.
func (p *Parser) parseField(depth int) error {
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
			if !p.Lenient {
				return p.errorf(tok.offset,
					"unsupported expression %q; expected a literal, a nested table or an 'name = value' field", tok.text)
			}
			// An identifier that is not a key starts a positional field whose
			// value is an expression ("f()", "string.format", "inf"). The
			// identifier itself has been consumed, so a block keyword that
			// opens the expression ("function") has to be counted here.
			blocks := 0
			if opensBlock(tok.text) {
				blocks = 1
			}
			value, err := p.skipValue(tok.offset, blocks)
			if err != nil {
				return err
			}
			p.storePositional(value)
			return nil
		}
		if p.StrictKeywords && reservedWords[tok.text] {
			return p.errorf(tok.offset,
				"reserved word %q cannot be used as a table key; write [%q] instead", tok.text, tok.text)
		}
		p.lex.next()
		if err := p.lex.err; err != nil {
			return err
		}
		value, err := p.parseFieldValue(depth)
		if err != nil {
			return err
		}
		p.sink.keyed(tok.text, value)
		return nil

	case tokenLBracket:
		openTok := tok
		p.lex.next()
		if err := p.lex.err; err != nil {
			return err
		}

		keyStart := p.lex.tok.offset
		key, err := p.parseValue(depth)
		if err != nil {
			if !p.Lenient {
				return err
			}
			return p.dropField(keyStart, depth)
		}

		if p.lex.tok.typ != tokenRBracket {
			if p.Lenient {
				// The key is an expression that only starts like a literal
				// ("[1 + 2]"). The parser has consumed its first operand, so
				// the rest of the key and the whole field are dropped.
				return p.dropField(keyStart, depth)
			}
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

		value, err := p.parseFieldValue(depth)
		if err != nil {
			return err
		}

		// A key that a Table cannot represent is dropped together with its
		// field in lenient mode: there is no placeholder that could record it.
		if key == nil {
			if p.Lenient {
				return nil
			}
			return p.errorf(openTok.offset, "table index is nil")
		}
		// Lua rejects a NaN key as well ("table index is NaN") and accepts an
		// infinite one. This package rejects both: a NaN key could never be
		// looked up again in the resulting Table, and an infinity has no
		// literal the encoder could write back.
		if f, isFloat := key.(float64); isFloat && (math.IsNaN(f) || math.IsInf(f, 0)) {
			if p.Lenient {
				return nil
			}
			return p.errorf(openTok.offset, "table key is NaN or infinite")
		}
		normalized, ok := normalizeKey(key)
		if !ok {
			if p.Lenient {
				return nil
			}
			// A nested table is the only key that reaches this point: a nil
			// and a non-finite float key are rejected above, and no other
			// expression parses as a key. It is named in Lua terms rather than
			// by its Go type, which differs between the representations ("a
			// *Table" and a map), so that both report the same problem.
			return p.errorf(openTok.offset, "a table cannot be used as a table key")
		}
		p.sink.keyed(normalized, value)
		return nil

	default:
		// Positional field: part of the array section.
		value, err := p.parseFieldValue(depth)
		if err != nil {
			return err
		}
		p.storePositional(value)
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
	case tokenOperator:
		return fmt.Sprintf("'%s'", t.text)
	default:
		return t.typ.String()
	}
}

func (p *Parser) errorf(offset int, format string, args ...any) error {
	return newSyntaxError(p.src, offset, format, args...)
}
