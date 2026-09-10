package luatable

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// parseLuaNumber converts a Lua numeric literal (Lua 5.1 - 5.4) into either an
// int64 (integer literal) or a float64 (float literal).
//
// Rules:
//
//   - decimal integers become int64; if the value does not fit into int64 it
//     degrades to float64, matching Lua 5.3+ behaviour;
//   - decimal literals containing '.' or an exponent become float64;
//   - hexadecimal integers become int64, wrapping around modulo 2^64 like Lua;
//   - hexadecimal literals with a fractional part or a binary exponent ("p")
//     become float64.
func parseLuaNumber(s string) (any, error) {
	if s == "" {
		return nil, fmt.Errorf("empty number literal")
	}
	if len(s) > 1 && s[0] == '0' && (s[1] == 'x' || s[1] == 'X') {
		return parseHexNumber(s)
	}
	return parseDecNumber(s)
}

func parseDecNumber(s string) (any, error) {
	if strings.ContainsAny(s, ".eE") {
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid number literal %q", s)
		}
		return f, nil
	}

	n, err := strconv.ParseInt(s, 10, 64)
	if err == nil {
		return n, nil
	}

	// The literal may be a valid decimal integer that overflows int64. Lua
	// promotes such literals to floating point values.
	f, ferr := strconv.ParseFloat(s, 64)
	if ferr != nil {
		return nil, fmt.Errorf("invalid number literal %q", s)
	}
	return f, nil
}

func parseHexNumber(s string) (any, error) {
	body := s[2:]
	if body == "" {
		return nil, fmt.Errorf("invalid number literal %q", s)
	}

	if strings.ContainsAny(body, ".pP") {
		return parseHexFloat(s, body)
	}

	u, err := parseHexUint64(body)
	if err != nil {
		return nil, fmt.Errorf("invalid number literal %q", s)
	}
	// Lua stores hexadecimal integers as unsigned values that wrap around
	// modulo 2^64 before being interpreted as signed integers.
	return int64(u), nil
}

// parseHexFloat parses the "0x" prefix stripped body of a hexadecimal float.
// The binary exponent introduced by 'p' is optional, so both "0x1.8" and
// "0x1.8p1" are accepted.
func parseHexFloat(orig, body string) (any, error) {
	mantissa := body
	exp := 0

	if i := strings.IndexAny(body, "pP"); i >= 0 {
		mantissa = body[:i]
		expStr := body[i+1:]
		if expStr == "" {
			return nil, fmt.Errorf("invalid number literal %q", orig)
		}
		e, err := strconv.Atoi(expStr)
		if err != nil {
			return nil, fmt.Errorf("invalid number literal %q", orig)
		}
		exp = e
	}

	intPart, fracPart := mantissa, ""
	if i := strings.IndexByte(mantissa, '.'); i >= 0 {
		intPart = mantissa[:i]
		fracPart = mantissa[i+1:]
	}
	if intPart == "" && fracPart == "" {
		return nil, fmt.Errorf("invalid number literal %q", orig)
	}

	value := 0.0
	for i := range len(intPart) {
		v, ok := hexVal(intPart[i])
		if !ok {
			return nil, fmt.Errorf("invalid number literal %q", orig)
		}
		value = value*16 + float64(v)
	}

	scale := 1.0 / 16.0
	for i := range len(fracPart) {
		v, ok := hexVal(fracPart[i])
		if !ok {
			return nil, fmt.Errorf("invalid number literal %q", orig)
		}
		value += float64(v) * scale
		scale /= 16
	}

	return math.Ldexp(value, exp), nil
}

// parseHexUint64 parses s as a hexadecimal unsigned integer, wrapping around
// modulo 2^64 on overflow. Leading zeros are accepted, but s must not be empty
// and must consist solely of hexadecimal digits.
func parseHexUint64(s string) (uint64, error) {
	if s == "" {
		return 0, fmt.Errorf("empty hexadecimal literal")
	}
	var u uint64
	for i := range len(s) {
		v, ok := hexVal(s[i])
		if !ok {
			return 0, fmt.Errorf("invalid hexadecimal digit %q", string(s[i]))
		}
		// Unsigned arithmetic in Go wraps around, which is exactly the
		// semantics required here.
		u = u*16 + uint64(v)
	}
	return u, nil
}

func hexVal(c byte) (int, bool) {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0'), true
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10, true
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10, true
	default:
		return 0, false
	}
}
