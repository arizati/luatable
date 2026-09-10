package luatable

import (
	"fmt"
	"strconv"
	"strings"
)

// decodeStringLiteral decodes a raw Lua string token (including its
// delimiters) into its value.
//
// On failure the returned offset locates the offending byte relative to the
// start of raw, allowing the parser to report an accurate position.
func decodeStringLiteral(raw string) (value string, errOffset int, err error) {
	if raw == "" {
		return "", 0, fmt.Errorf("empty string literal")
	}
	switch raw[0] {
	case '"', '\'':
		return decodeShortString(raw)
	case '[':
		return decodeLongString(raw)
	default:
		return "", 0, fmt.Errorf("malformed string literal %q", truncate(raw, 32))
	}
}

// decodeShortString decodes a quoted short string, interpreting every escape
// sequence supported by Lua 5.1 - 5.5.
func decodeShortString(raw string) (string, int, error) {
	body := raw[1 : len(raw)-1]

	if strings.IndexByte(body, '\\') < 0 {
		// Fast path: nothing to unescape.
		return body, 0, nil
	}

	var b strings.Builder
	b.Grow(len(body))

	for i := 0; i < len(body); {
		c := body[i]
		if c != '\\' {
			b.WriteByte(c)
			i++
			continue
		}

		// raw offset of the escape sequence, i.e. of the backslash.
		escapeOffset := i + 1
		i++ // consume the backslash
		if i >= len(body) {
			return "", escapeOffset, fmt.Errorf("unfinished escape sequence")
		}

		switch e := body[i]; e {
		case 'a':
			b.WriteByte('\a')
			i++
		case 'b':
			b.WriteByte('\b')
			i++
		case 'f':
			b.WriteByte('\f')
			i++
		case 'n':
			b.WriteByte('\n')
			i++
		case 'r':
			b.WriteByte('\r')
			i++
		case 't':
			b.WriteByte('\t')
			i++
		case 'v':
			b.WriteByte('\v')
			i++
		case '\\':
			b.WriteByte('\\')
			i++
		case '"':
			b.WriteByte('"')
			i++
		case '\'':
			b.WriteByte('\'')
			i++
		case '\n':
			b.WriteByte('\n')
			i++
		case '\r':
			if i+1 < len(body) && body[i+1] == '\n' {
				i += 2
			} else {
				i++
			}
			b.WriteByte('\n')
		case 'x':
			if i+3 > len(body) {
				return "", escapeOffset, fmt.Errorf("hexadecimal digit expected after '\\x'")
			}
			v, err := strconv.ParseUint(body[i+1:i+3], 16, 8)
			if err != nil {
				return "", escapeOffset, fmt.Errorf("hexadecimal digit expected after '\\x'")
			}
			b.WriteByte(byte(v))
			i += 3
		case 'z':
			i++
			for i < len(body) && isSpace(body[i]) {
				i++
			}
		case 'u':
			val, next, err := decodeUnicodeEscape(body, i)
			if err != nil {
				return "", escapeOffset, err
			}
			b.WriteRune(rune(val))
			i = next
		case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
			v := 0
			n := 0
			for i < len(body) && n < 3 && isDigit(body[i]) {
				v = v*10 + int(body[i]-'0')
				i++
				n++
			}
			if v > 255 {
				return "", escapeOffset, fmt.Errorf("decimal escape sequence too large")
			}
			b.WriteByte(byte(v))
		default:
			return "", escapeOffset, fmt.Errorf("invalid escape sequence '\\%c'", e)
		}
	}

	return b.String(), 0, nil
}

// decodeUnicodeEscape decodes the "\u{XXXX}" escape starting at the index of
// the 'u' character. It returns the code point and the index just past the
// closing brace.
//
// Code points are limited to the Unicode scalar range: Lua 5.4 itself accepts
// values up to 0x7FFFFFFF, but those beyond U+10FFFF (and surrogate halves)
// have no representation as a single Go rune.
func decodeUnicodeEscape(body string, i int) (uint64, int, error) {
	if i+1 >= len(body) || body[i+1] != '{' {
		return 0, 0, fmt.Errorf("missing '{' in '\\u{...}' escape")
	}
	j := i + 2
	for j < len(body) && body[j] != '}' {
		j++
	}
	if j >= len(body) {
		return 0, 0, fmt.Errorf("missing '}' in '\\u{...}' escape")
	}

	hexDigits := body[i+2 : j]
	if hexDigits == "" || len(hexDigits) > 8 {
		return 0, 0, fmt.Errorf("invalid '\\u{...}' escape")
	}
	val, err := strconv.ParseUint(hexDigits, 16, 32)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid '\\u{...}' escape")
	}
	if val > 0x10FFFF || (val >= 0xD800 && val <= 0xDFFF) {
		return 0, 0, fmt.Errorf("invalid Unicode code point in '\\u{...}' escape")
	}
	return val, j + 1, nil
}

// decodeLongString decodes a long string literal, stripping its delimiters and
// the newline that immediately follows the opening delimiter, as mandated by
// the Lua specification. No escape sequences are interpreted.
func decodeLongString(raw string) (string, int, error) {
	i := 1
	for i < len(raw) && raw[i] == '=' {
		i++
	}
	if i >= len(raw) || raw[i] != '[' {
		return "", 0, fmt.Errorf("malformed long string literal")
	}

	delim := i + 1
	if len(raw) < 2*delim {
		return "", 0, fmt.Errorf("malformed long string literal")
	}

	content := raw[delim : len(raw)-delim]

	// A newline immediately following the opening bracket is ignored.
	switch {
	case strings.HasPrefix(content, "\r\n"), strings.HasPrefix(content, "\n\r"):
		content = content[2:]
	case strings.HasPrefix(content, "\n"), strings.HasPrefix(content, "\r"):
		content = content[1:]
	}

	return content, 0, nil
}
