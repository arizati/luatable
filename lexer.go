package luatable

import "unicode/utf8"

// tokenType enumerates the lexical classes produced by the lexer.
type tokenType uint8

const (
	tokenEOF       tokenType = iota // end of input
	tokenLBrace                     // {
	tokenRBrace                     // }
	tokenLBracket                   // [
	tokenRBracket                   // ]
	tokenAssign                     // =
	tokenComma                      // ,
	tokenSemicolon                  // ;
	tokenLParen                     // (
	tokenRParen                     // )
	tokenMinus                      // -
	tokenName                       // identifier, e.g. foo_bar1
	tokenKeyword                    // nil, true, false, return
	tokenNumber                     // numeric literal
	tokenString                     // string literal (raw text, including delimiters)
)

// String returns a human readable description of t, suitable for error
// messages.
func (t tokenType) String() string {
	switch t {
	case tokenEOF:
		return "end of input"
	case tokenLBrace:
		return "'{'"
	case tokenRBrace:
		return "'}'"
	case tokenLBracket:
		return "'['"
	case tokenRBracket:
		return "']'"
	case tokenAssign:
		return "'='"
	case tokenComma:
		return "','"
	case tokenSemicolon:
		return "';'"
	case tokenLParen:
		return "'('"
	case tokenRParen:
		return "')'"
	case tokenMinus:
		return "'-'"
	case tokenName:
		return "identifier"
	case tokenKeyword:
		return "keyword"
	case tokenNumber:
		return "number literal"
	case tokenString:
		return "string literal"
	default:
		return "unknown token"
	}
}

// token is a single lexical element together with its source position.
//
// For tokenString the Text field holds the raw literal including its
// delimiters; decoding is performed by decodeStringLiteral. For tokenNumber
// the Text field holds the literal text; conversion is performed by
// parseLuaNumber.
type token struct {
	typ    tokenType
	text   string
	offset int
}

// lexer performs on-demand tokenization of a Lua table constructor.
//
// The lexer is intentionally small: it only knows the lexical grammar shared
// by Lua 5.1 - 5.4 (whitespace, comments, long brackets, strings and numbers)
// and leaves all grammar decisions to the parser.
type lexer struct {
	src string
	pos int
	err *SyntaxError

	// tok holds the most recently scanned token.
	tok token
}

// next advances the lexer to the next token, skipping whitespace and
// comments. After the call exactly one of l.err or l.tok is meaningful: when
// l.err is non-nil it describes the failure, otherwise l.tok holds the scanned
// token (tokenEOF at the end of input).
func (l *lexer) next() {
	l.skipSpaceAndComments()
	l.tok = token{typ: tokenEOF, offset: l.pos}
	if l.err != nil {
		return
	}
	if l.pos >= len(l.src) {
		return
	}

	start := l.pos
	c := l.src[l.pos]
	switch {
	case c == '{':
		l.pos++
		l.tok = token{typ: tokenLBrace, text: "{", offset: start}
	case c == '}':
		l.pos++
		l.tok = token{typ: tokenRBrace, text: "}", offset: start}
	case c == '[':
		if level, ok := l.longBracketLevel(l.pos); ok {
			raw, _, ok := l.readLongBracket(level)
			if !ok {
				return
			}
			l.tok = token{typ: tokenString, text: raw, offset: start}
			return
		}
		l.pos++
		l.tok = token{typ: tokenLBracket, text: "[", offset: start}
	case c == ']':
		l.pos++
		l.tok = token{typ: tokenRBracket, text: "]", offset: start}
	case c == '=':
		l.pos++
		l.tok = token{typ: tokenAssign, text: "=", offset: start}
	case c == ',':
		l.pos++
		l.tok = token{typ: tokenComma, text: ",", offset: start}
	case c == ';':
		l.pos++
		l.tok = token{typ: tokenSemicolon, text: ";", offset: start}
	case c == '(':
		l.pos++
		l.tok = token{typ: tokenLParen, text: "(", offset: start}
	case c == ')':
		l.pos++
		l.tok = token{typ: tokenRParen, text: ")", offset: start}
	case c == '-':
		l.pos++
		l.tok = token{typ: tokenMinus, text: "-", offset: start}
	case c == '"' || c == '\'':
		l.scanShortString()
	case isDigit(c) || (c == '.' && isDigit(l.peek(1))):
		l.scanNumber()
	case isNameStart(c):
		l.scanName()
	default:
		r, _ := utf8.DecodeRuneInString(l.src[start:])
		l.err = newSyntaxError(l.src, start, "unexpected character %q", r)
	}
}

// skipSpaceAndComments advances past whitespace, line comments and block
// comments, stopping at the first character that begins a token.
func (l *lexer) skipSpaceAndComments() {
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		if isSpace(c) {
			l.pos++
			continue
		}
		if c == '-' && l.peek(1) == '-' {
			l.pos += 2
			// A block comment is introduced by "--" followed by a long
			// bracket. Anything else is a line comment.
			if l.pos < len(l.src) && l.src[l.pos] == '[' {
				if level, ok := l.longBracketLevel(l.pos); ok {
					if _, _, ok := l.readLongBracket(level); !ok {
						return
					}
					continue
				}
			}
			for l.pos < len(l.src) && l.src[l.pos] != '\n' && l.src[l.pos] != '\r' {
				l.pos++
			}
			continue
		}
		return
	}
}

// peek returns the byte at offset off relative to the current position, or 0
// when it lies outside the input.
func (l *lexer) peek(off int) byte {
	i := l.pos + off
	if i < 0 || i >= len(l.src) {
		return 0
	}
	return l.src[i]
}

// longBracketLevel reports the level of a long bracket starting at index i
// (which must point at '['). A long bracket looks like "[", a run of zero or
// more '=' and a terminating '['. The level is the number of '=' characters.
func (l *lexer) longBracketLevel(i int) (int, bool) {
	if i >= len(l.src) || l.src[i] != '[' {
		return 0, false
	}
	j := i + 1
	for j < len(l.src) && l.src[j] == '=' {
		j++
	}
	if j < len(l.src) && l.src[j] == '[' {
		return j - i - 1, true
	}
	return 0, false
}

// readLongBracket consumes a long bracket of the given level starting at the
// current position. It returns the raw text (including delimiters) and the
// inner content. On failure l.err is set and ok is false.
func (l *lexer) readLongBracket(level int) (raw, content string, ok bool) {
	start := l.pos
	l.pos += level + 2
	contentStart := l.pos

	for {
		if l.pos >= len(l.src) {
			l.err = newSyntaxError(l.src, start, "unfinished long bracket")
			return "", "", false
		}
		if l.src[l.pos] == ']' {
			j := l.pos + 1
			n := 0
			for n < level && j < len(l.src) && l.src[j] == '=' {
				j++
				n++
			}
			if n == level && j < len(l.src) && l.src[j] == ']' {
				contentEnd := l.pos
				rawEnd := j + 1
				raw = l.src[start:rawEnd]
				content = l.src[contentStart:contentEnd]
				l.pos = rawEnd
				return raw, content, true
			}
		}
		l.pos++
	}
}

// scanShortString scans a single- or double-quoted string literal. Escape
// sequences are skipped but not interpreted; decoding happens later.
func (l *lexer) scanShortString() {
	start := l.pos
	quote := l.src[l.pos]
	l.pos++

	for l.pos < len(l.src) {
		c := l.src[l.pos]
		switch {
		case c == quote:
			l.pos++
			l.tok = token{typ: tokenString, text: l.src[start:l.pos], offset: start}
			return
		case c == '\\':
			l.pos++
			if l.pos >= len(l.src) {
				break
			}
			// "\<newline>" is a valid line continuation.
			if l.src[l.pos] == '\r' && l.peek(1) == '\n' {
				l.pos += 2
				continue
			}
			l.pos++
			continue
		case c == '\n' || c == '\r':
			l.err = newSyntaxError(l.src, start, "unfinished string literal")
			return
		default:
			l.pos++
		}
	}
	l.err = newSyntaxError(l.src, start, "unfinished string literal")
}

// scanNumber scans a decimal or hexadecimal numeric literal. Validation of the
// scanned text is delegated to parseLuaNumber.
func (l *lexer) scanNumber() {
	start := l.pos

	if l.src[l.pos] == '0' && (l.peek(1) == 'x' || l.peek(1) == 'X') {
		l.pos += 2
		l.scanHexDigits()
		if l.peek(0) == '.' {
			l.pos++
			l.scanHexDigits()
		}
		if c := l.peek(0); c == 'p' || c == 'P' {
			l.pos++
			if c := l.peek(0); c == '+' || c == '-' {
				l.pos++
			}
			l.scanDecDigits()
		}
	} else {
		l.scanDecDigits()
		if l.peek(0) == '.' {
			l.pos++
			l.scanDecDigits()
		}
		if c := l.peek(0); c == 'e' || c == 'E' {
			l.pos++
			if c := l.peek(0); c == '+' || c == '-' {
				l.pos++
			}
			l.scanDecDigits()
		}
	}

	l.tok = token{typ: tokenNumber, text: l.src[start:l.pos], offset: start}
}

func (l *lexer) scanDecDigits() {
	for l.pos < len(l.src) && isDigit(l.src[l.pos]) {
		l.pos++
	}
}

func (l *lexer) scanHexDigits() {
	for l.pos < len(l.src) && isHexDigit(l.src[l.pos]) {
		l.pos++
	}
}

// scanName scans an identifier and classifies reserved words as keywords.
func (l *lexer) scanName() {
	start := l.pos
	l.pos++
	for l.pos < len(l.src) && isNameChar(l.src[l.pos]) {
		l.pos++
	}
	text := l.src[start:l.pos]
	typ := tokenName
	if isKeyword(text) {
		typ = tokenKeyword
	}
	l.tok = token{typ: typ, text: text, offset: start}
}

func isSpace(c byte) bool {
	switch c {
	case ' ', '\t', '\n', '\r', '\v', '\f':
		return true
	default:
		return false
	}
}

func isDigit(c byte) bool {
	return c >= '0' && c <= '9'
}

func isHexDigit(c byte) bool {
	return isDigit(c) || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

func isNameStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isNameChar(c byte) bool {
	return isNameStart(c) || isDigit(c)
}

// keywords lists the words the parser must recognise as primitives: nil, true
// and false are literal values and return introduces a module file.
//
// The set is deliberately smaller than Lua's reserved words. A lenient data
// parser accepts "end = 1" as a string key even though every Lua
// implementation rejects it; set Parser.StrictKeywords to reject such keys.
var keywords = map[string]bool{
	"nil":    true,
	"true":   true,
	"false":  true,
	"return": true,
}

func isKeyword(s string) bool {
	return keywords[s]
}

// reservedWords lists every reserved word of Lua 5.1 through Lua 5.5.
//
// The set is the union over all supported versions, which is conservative in
// the right direction: writing '["goto"]' or '["global"]' is valid in every
// version, whereas a bare "goto = 1" or "global = 1" is rejected by some of
// them. "goto" became reserved in Lua 5.2 and "global" in Lua 5.5:
//
//	and break do else elseif end false for function global goto if
//	in local nil not or repeat return then true until while
var reservedWords = map[string]bool{
	"and": true, "break": true, "do": true, "else": true, "elseif": true,
	"end": true, "false": true, "for": true, "function": true, "global": true,
	"goto": true, "if": true, "in": true, "local": true, "nil": true,
	"not": true, "or": true, "repeat": true, "return": true, "then": true,
	"true": true, "until": true, "while": true,
}

// isIdentifier reports whether s is a Lua Name: a well-formed identifier that
// is not a reserved word, and therefore may be written without brackets as a
// table key.
func isIdentifier(s string) bool {
	if s == "" || !isNameStart(s[0]) {
		return false
	}
	for i := 1; i < len(s); i++ {
		if !isNameChar(s[i]) {
			return false
		}
	}
	return !reservedWords[s]
}
