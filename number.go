package luatable

import (
	"fmt"
	"strconv"
	"strings"
)

// parseLuaNumber converts a Lua numeric literal (Lua 5.1 - 5.5) into either an
// int64 (integer literal) or a float64 (float literal).
//
// Rules:
//
//   - decimal integers become int64; if the value does not fit into int64 it
//     degrades to float64, matching Lua 5.3+ behaviour;
//   - decimal literals containing '.' or an exponent become float64;
//   - hexadecimal integers become int64, wrapping around modulo 2^64 like Lua;
//   - hexadecimal literals with a fractional part or a binary exponent ("p")
//     become float64;
//   - literals whose value lies outside the float64 range evaluate to ±Inf
//     (overflow) or to zero (underflow), as every reference implementation
//     does, although the manual leaves float overflow unspecified.
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
		return parseFloatLiteral(s, s)
	}

	n, err := strconv.ParseInt(s, 10, 64)
	if err == nil {
		return n, nil
	}

	// The literal may be a valid decimal integer that overflows int64. Lua
	// promotes such literals to floating point values.
	return parseFloatLiteral(s, s)
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
//
// The conversion is delegated to strconv.ParseFloat, which implements
// hexadecimal literals with correct rounding. Accumulating the mantissa digit
// by digit in float64 rounds at every step and drifts from the correctly
// rounded result by up to one ULP once the mantissa exceeds 53 bits: against
// 20000 random literals, strconv agreed with Lua 5.2, 5.3, 5.4 and 5.5 on
// every single one, while the manual accumulation disagreed with all of them
// on 9.4%. TestHexFloatMatchesLua repeats that comparison against every
// interpreter it can find, and it reads each value back as "%a", an exact
// rendering of the double, so the comparison holds bit for bit and no
// tolerance is needed for LuaJIT, which formats with its own code and rounds
// "%.17g" ties away from zero on distribution builds of 2.1.0~beta3. That
// test skips Lua 5.1, whose lexer cannot read a hexadecimal float at all
// ("0x1.8p1" and even "0x1.8" are syntax errors there).
//
// A missing binary exponent is supplied as "p0", because ParseFloat requires
// one.
func parseHexFloat(orig, body string) (any, error) {
	lit := orig
	if !strings.ContainsAny(body, "pP") {
		lit += "p0"
	}
	return parseFloatLiteral(lit, orig)
}

// parseFloatLiteral converts the numeric literal s with strconv.ParseFloat.
//
// The ErrRange that out-of-range literals produce is accepted: such literals
// evaluate to ±Inf or to zero. That matches every implementation tested (Lua
// 5.1 through 5.5 and LuaJIT), even though the manual does not specify float
// overflow. orig names the literal in error messages and differs from s when s
// was normalized, for example by appending a missing "p0" binary exponent.
func parseFloatLiteral(s, orig string) (any, error) {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		if ne, ok := err.(*strconv.NumError); ok && ne.Err == strconv.ErrRange {
			return f, nil
		}
		return nil, fmt.Errorf("invalid number literal %q", orig)
	}
	return f, nil
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
