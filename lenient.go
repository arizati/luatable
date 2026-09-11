package luatable

import "strings"

// Skipped records a table field value that the lenient parser consumed without
// decoding it: a function call, a name reference, an arithmetic or
// concatenation expression, a function literal, or any other token sequence
// that is not a literal or a nested table.
//
// A Skipped value appears wherever the decoded value would have been stored, in
// the generic representation as well as in a *Table, so a skipped positional
// field never loses its array index. Marshal rejects it, because writing the
// raw text back could emit code or text that is not valid Lua; set
// Encoder.EmitSkipped to write the text back anyway.
type Skipped struct {
	// Text is the raw source text of the value. It is a substring of the
	// parsed input and therefore copies nothing, but it keeps the whole input
	// string reachable for as long as it is referenced.
	Text string

	// Offset is the byte offset at which the value starts. It is positional
	// metadata about the text that was parsed, so it differs between two
	// parses of the same data; compare Text when comparing values.
	Offset int
}

// String returns a short description of the skipped value, for debugging and
// for error messages.
func (s Skipped) String() string {
	return "<skipped: " + truncate(s.Text, 32) + ">"
}

// startsLiteral reports whether tok can begin one of the values the strict
// parser accepts.
func startsLiteral(tok token) bool {
	switch tok.typ {
	case tokenNumber, tokenString, tokenLBrace, tokenLParen, tokenMinus:
		return true
	case tokenKeyword:
		return tok.text == "nil" || tok.text == "true" || tok.text == "false"
	default:
		return false
	}
}

// endsField reports whether tok terminates a field of a table constructor.
func endsField(tok token) bool {
	return tok.typ == tokenComma || tok.typ == tokenSemicolon || tok.typ == tokenRBrace
}

// opensBlock reports whether name is a keyword that opens a block. The lexer
// classifies only nil, true, false and return as keywords, because those are
// the only ones the strict grammar needs, so the block keywords arrive as
// identifiers.
func opensBlock(name string) bool {
	switch name {
	case "do", "function", "if", "repeat":
		return true
	default:
		return false
	}
}

// parseFieldValue parses the value of a table field. In strict mode it is
// exactly Parser.parseValue. In lenient mode a value that cannot be decoded as
// a literal or a nested table is consumed and returned as a Skipped, either
// because it does not start like a literal ("f()"), because a decoded literal
// turned out to be the left operand of a larger expression ("1 + 2"), or
// because decoding it failed ("-f()").
//
// The caller stores the result, so a skipped positional field still occupies
// its array index.
func (p *Parser) parseFieldValue(depth int) (any, error) {
	if !p.Lenient {
		return p.parseValue(depth)
	}

	start := p.lex.tok.offset
	if !startsLiteral(p.lex.tok) {
		return p.skipValue(start, 0)
	}

	value, err := p.parseValue(depth)
	if err != nil || !endsField(p.lex.tok) {
		// The value is undecodable, or it continues past the decoded literal.
		// Everything from start to the field separator becomes one Skipped
		// value; the tokens a failed parse consumed are part of that span.
		return p.skipValue(start, 0)
	}
	return value, nil
}

// skipValue consumes one field value that cannot be decoded and records it as a
// Skipped. start is the byte offset at which the value begins, and blocks is
// the number of block keywords already consumed as part of the value, which is
// non-zero when a positional field starts with a function literal and the
// caller had to look at the token behind it. Tokens that a failed parse already
// consumed are part of the recorded text.
func (p *Parser) skipValue(start, blocks int) (Skipped, error) {
	end, err := p.skipTokens(start, blocks, "value", endsField)
	if err != nil {
		return Skipped{}, err
	}
	if end == start {
		return Skipped{}, p.errorf(start, "expected a value")
	}
	text := strings.TrimRight(p.src[start:end], " \t\r\n\v\f")
	return Skipped{Text: text, Offset: start}, nil
}

// skipTokens consumes tokens until stop accepts one that lies outside every
// nesting level, and returns the offset at which that token starts. The token
// itself is not consumed. what names the skipped construct in error messages.
//
// Nesting is tracked with counters instead of recursion, so hostile input
// cannot exhaust the stack, and every iteration consumes one token, so the scan
// always terminates. Block keywords are counted as well: a comma inside a
// function body ("function() return 1, 2 end") does not separate table fields.
// A closing token without a matching opener is consumed rather than rejected,
// which keeps the recovery going over input that is not valid Lua at all;
// running out of input is reported as an error.
func (p *Parser) skipTokens(start, blocks int, what string, stop func(token) bool) (int, error) {
	var parens, brackets, braces int

	for {
		// A pending lexical error names the real problem and its position; the
		// token it left behind is a synthetic EOF, which must not replace the
		// diagnosis with "end of input".
		if err := p.lex.err; err != nil {
			return 0, err
		}
		tok := p.lex.tok
		if tok.typ == tokenEOF {
			return 0, p.errorf(start, "unexpected end of input while skipping the %s", what)
		}
		if parens == 0 && brackets == 0 && braces == 0 && blocks == 0 && stop(tok) {
			return tok.offset, nil
		}

		switch tok.typ {
		case tokenLParen:
			parens++
		case tokenRParen:
			if parens > 0 {
				parens--
			}
		case tokenLBracket:
			brackets++
		case tokenRBracket:
			if brackets > 0 {
				brackets--
			}
		case tokenLBrace:
			braces++
		case tokenRBrace:
			if braces > 0 {
				braces--
			}
		case tokenName, tokenKeyword:
			// The block keywords arrive as identifiers, because the lexer
			// classifies only the keywords the strict grammar needs.
			switch {
			case opensBlock(tok.text):
				blocks++
			case tok.text == "end" || tok.text == "until":
				if blocks > 0 {
					blocks--
				}
			}
		}

		p.lex.next()
		if err := p.lex.err; err != nil {
			return 0, err
		}
	}
}

// dropField consumes a field whose bracketed key is not a usable literal and
// stores nothing: table keys are literals, so there is no placeholder that
// could record such a key. keyStart is the offset at which the key expression
// begins and depth is the current nesting depth; the opening '[' has already
// been consumed.
func (p *Parser) dropField(keyStart, depth int) error {
	stopAtBracket := func(tok token) bool { return tok.typ == tokenRBracket }
	if _, err := p.skipTokens(keyStart, 0, "table key", stopAtBracket); err != nil {
		return err
	}

	p.lex.next() // consume the ']' that skipTokens stopped at
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

	// Consume the value as well, so that parsing continues after the field.
	_, err := p.parseFieldValue(depth)
	return err
}
