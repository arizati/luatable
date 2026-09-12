package luatable

import (
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
)

// This file targets the branches that the broader integration tests do not
// reach: lexical errors that surface in the middle of a parse, malformed
// literals, defensive switches, and the nil or zero-value receivers that the
// public helpers never construct.

func TestTokenTypeString(t *testing.T) {
	cases := []struct {
		typ  tokenType
		want string
	}{
		{tokenEOF, "end of input"},
		{tokenLBrace, "'{'"},
		{tokenRBrace, "'}'"},
		{tokenLBracket, "'['"},
		{tokenRBracket, "']'"},
		{tokenAssign, "'='"},
		{tokenComma, "','"},
		{tokenSemicolon, "';'"},
		{tokenLParen, "'('"},
		{tokenRParen, "')'"},
		{tokenMinus, "'-'"},
		{tokenName, "identifier"},
		{tokenKeyword, "keyword"},
		{tokenNumber, "number literal"},
		{tokenString, "string literal"},
		{tokenOperator, "operator"},
		{tokenType(200), "unknown token"},
	}
	for _, tc := range cases {
		if got := tc.typ.String(); got != tc.want {
			t.Errorf("tokenType(%d).String() = %q; want %q", tc.typ, got, tc.want)
		}
	}
}

func TestLongBracketLevelOutOfRange(t *testing.T) {
	l := &lexer{src: "x["}
	if _, ok := l.longBracketLevel(len(l.src)); ok {
		t.Fatal("longBracketLevel past the end of input must report false")
	}
	if _, ok := l.longBracketLevel(len(l.src) + 100); ok {
		t.Fatal("longBracketLevel far past the end of input must report false")
	}
}

// TestLexerLineContinuationCRLF covers the "\<CR><LF>" continuation, which the
// lexer consumes as one newline so that the string stays open.
func TestLexerLineContinuationCRLF(t *testing.T) {
	src := "\"a\\\r\nb\"" // "a\<CR><LF>b"
	got := lexAll(t, src)
	if len(got) != 1 {
		t.Fatalf("unexpected token count; got %d; want 1", len(got))
	}
	if got[0].typ != tokenString || got[0].text != src {
		t.Fatalf("unexpected token %#v; want the raw text %q", got[0], src)
	}

	value, _, err := decodeStringLiteral(got[0].text)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if value != "a\nb" {
		t.Fatalf("decoded value = %q; want %q", value, "a\nb")
	}
}

// TestDecodeShortStringLineContinuations checks every newline form Lua allows
// after a backslash: LF, CRLF and a lone CR all decode to a single '\n'.
func TestDecodeShortStringLineContinuations(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{"LF", "\"a\\\nb\""},
		{"CRLF", "\"a\\\r\nb\""},
		{"CR", "\"a\\\rb\""},
		{"LFCR", "\"a\\\n\rb\""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, _, err := decodeStringLiteral(tc.raw)
			if err != nil {
				t.Fatalf("unexpected error: %s", err)
			}
			if got != "a\nb" {
				t.Fatalf("decoded value = %q; want %q", got, "a\nb")
			}
		})
	}

	t.Run("through the parser", func(t *testing.T) {
		for _, tc := range cases {
			value, err := Parse("{a = " + tc.raw + "}")
			if err != nil {
				t.Fatalf("unexpected error for %s: %s", tc.name, err)
			}
			if got := value.(map[string]any)["a"]; got != "a\nb" {
				t.Fatalf("%s: value = %q; want %q", tc.name, got, "a\nb")
			}
		}
	})
}

func TestDecodeStringLiteralMalformed(t *testing.T) {
	short := []string{"abc", "123", "}"}
	for _, raw := range short {
		if _, _, err := decodeStringLiteral(raw); err == nil {
			t.Errorf("expecting an error for %q", raw)
		} else if !strings.Contains(err.Error(), "malformed string literal") {
			t.Errorf("error for %q = %v; want it to mention a malformed literal", raw, err)
		}
	}

	long := []string{"[=x", "[=", "[", "[[]", "[=]"}
	for _, raw := range long {
		if _, _, err := decodeStringLiteral(raw); err == nil {
			t.Errorf("expecting an error for %q", raw)
		} else if !strings.Contains(err.Error(), "malformed long string literal") {
			t.Errorf("error for %q = %v; want it to mention a malformed long string", raw, err)
		}
	}
}

func TestParseHexUint64InvalidDigit(t *testing.T) {
	if _, err := parseHexUint64(""); err == nil {
		t.Error("expecting an error for the empty string")
	}
	for _, s := range []string{"g", "12g", "0x12"} {
		if _, err := parseHexUint64(s); err == nil {
			t.Errorf("expecting an error for %q", s)
		}
	}
}

// TestParseReportsMidParseErrors pins the position and message of the lexical
// and numeric errors that can interrupt a parse after the constructor is under
// way: each case reaches a different recovery point inside the parser.
func TestParseReportsMidParseErrors(t *testing.T) {
	cases := []struct {
		name        string
		src         string
		wantMsg     string
		lenient     bool
		allowReturn bool
	}{
		{"after the return prefix", `return "abc`, "unfinished string literal", false, true},
		{"after the empty constructor", `{} "abc`, "unfinished string literal", false, false},
		{"after a trailing semicolon", `{}; "abc`, "unfinished string literal", false, false},
		{"after a closing brace", `{1} "abc`, "unfinished string literal", false, false},
		{"after a trailing separator", `{1,} "abc`, "unfinished string literal", false, false},
		{"after a field name", `{abc "abc`, "unfinished string literal", false, false},
		{"while skipping a positional expression", `{abc def "abc`, "unfinished string literal", true, false},
		{"after the opening bracket of a key", `{["abc`, "unfinished string literal", false, false},
		{"after the closing bracket of a key", `{["a"] "abc`, "unfinished string literal", false, false},
		{"after the equals sign of a key", `{["a"] = "abc`, "unfinished string literal", false, false},
		{"from a bracketed field value", `{["a"] = f()}`, `unsupported expression "f"`, false, false},
		{"after a number value", `{1 "abc`, "unfinished string literal", false, false},
		{"from a malformed number", `{a = 0x}`, `invalid number literal "0x"`, false, false},
		{"after a string value", `{"a" "abc`, "unfinished string literal", false, false},
		{"from a malformed escape", `{"\q"}`, "invalid escape sequence", false, false},
		{"after a keyword value", `{nil "abc`, "unfinished string literal", false, false},
		{"after a unary minus", `{- "abc`, "unfinished string literal", false, false},
		{"after an opening parenthesis", `{( "abc`, "unfinished string literal", false, false},
		{"after a parenthesized value", `{(1) "abc`, "unfinished string literal", false, false},
		{"where a value is required", `{a = `, "unexpected end of input, expected a value", false, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var p Parser
			p.Lenient = tc.lenient
			p.AllowReturnPrefix = tc.allowReturn

			_, err := p.ParseTable(tc.src)
			if err == nil {
				t.Fatalf("expecting an error for %q", tc.src)
			}
			var se *SyntaxError
			if !errors.As(err, &se) {
				t.Fatalf("error %T is not a *SyntaxError: %s", err, err)
			}
			if !strings.Contains(se.Msg, tc.wantMsg) {
				t.Fatalf("unexpected message %q; want it to contain %q", se.Msg, tc.wantMsg)
			}
		})
	}
}

// TestParseNegatedHexMinimum covers the int64 overflow branch of negateNumber:
// only the hexadecimal form denotes math.MinInt64 as an int64, because the
// decimal form is promoted to float64 while it is being scanned.
func TestParseNegatedHexMinimum(t *testing.T) {
	got := parseGeneric(t, `{-0x8000000000000000}`)
	want := []any{-float64(math.MinInt64)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected value; got %#v; want %#v", got, want)
	}
}

// TestParseValueRejectsUnexpectedKeyword exercises the defensive default of the
// keyword switch in parseValue: the lexer only classifies nil, true, false and
// return as keywords, so the branch is fed a synthetic token.
func TestParseValueRejectsUnexpectedKeyword(t *testing.T) {
	p := &Parser{}
	p.lex = lexer{src: ""}
	p.lex.tok = token{typ: tokenKeyword, text: "and"}

	_, err := p.parseValue(1)
	if err == nil {
		t.Fatal("expecting an error")
	}
	if !strings.Contains(err.Error(), `unexpected keyword "and"`) {
		t.Fatalf("unexpected error: %s", err)
	}
}

func TestDescribeToken(t *testing.T) {
	cases := []struct {
		tok  token
		want string
	}{
		{token{typ: tokenName, text: "foo"}, `identifier "foo"`},
		{token{typ: tokenKeyword, text: "nil"}, `"nil"`},
		{token{typ: tokenNumber, text: "1.5"}, `number literal "1.5"`},
		{token{typ: tokenOperator, text: "+"}, `'+'`},
		{token{typ: tokenLBrace}, `'{'`},
	}

	p := &Parser{}
	for _, tc := range cases {
		p.lex.tok = tc.tok
		if got := p.describeToken(); got != tc.want {
			t.Errorf("describeToken(%#v) = %q; want %q", tc.tok, got, tc.want)
		}
	}
}

// TestLenientSkipsBracketedExpressions covers the bracket depth counters of
// skipTokens, which are only exercised when a skipped value contains an index.
func TestLenientSkipsBracketedExpressions(t *testing.T) {
	cases := []struct {
		src  string
		want string
	}{
		{`{a = t[1], b = 3}`, "t[1]"},
		{`{a = t[i][j], b = 3}`, "t[i][j]"},
		{`{a = f(x[1]) + y, b = 3}`, "f(x[1]) + y"},
	}

	for _, tc := range cases {
		t.Run(tc.src, func(t *testing.T) {
			var p Parser
			p.Lenient = true
			tbl, err := p.ParseTable(tc.src)
			if err != nil {
				t.Fatalf("unexpected error: %s", err)
			}
			if got := skippedAt(t, tbl, "a").Text; got != tc.want {
				t.Fatalf("Skipped.Text = %q; want %q", got, tc.want)
			}
			if got, _ := tbl.Get("b"); got != int64(3) {
				t.Fatalf("b = %#v; want 3", got)
			}
		})
	}
}

// TestLenientDropFieldErrors covers the error paths of dropField, which
// recovers from a bracketed key that is not a literal.
func TestLenientDropFieldErrors(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		wantMsg string
	}{
		{"lexical error after the key", `{[f()] @ = 1, ok = 2}`, "unexpected character '@'"},
		{"missing equals after the key", `{[f()] x = 1, ok = 2}`, "expected '=' after the table key"},
		{"lexical error after the equals", `{[f()] = "abc, ok = 2}`, "unfinished string literal"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var p Parser
			p.Lenient = true

			_, err := p.ParseTable(tc.src)
			if err == nil {
				t.Fatalf("expecting an error for %q", tc.src)
			}
			if !strings.Contains(err.Error(), tc.wantMsg) {
				t.Fatalf("error %v does not mention %q", err, tc.wantMsg)
			}
		})
	}
}

func TestGetPathNilTable(t *testing.T) {
	var tbl *Table
	if value, ok := tbl.GetPath("a"); ok || value != nil {
		t.Fatalf("GetPath on a nil table = %#v, %v; want nil, false", value, ok)
	}
}

func TestTableNilAndZeroValueReceivers(t *testing.T) {
	var nilTable *Table
	if got := nilTable.Entries(); got != nil {
		t.Errorf("Entries() on a nil table = %#v; want nil", got)
	}
	if got := nilTable.Map(); len(got) != 0 {
		t.Errorf("Map() on a nil table = %#v; want an empty map", got)
	}

	var zero Table
	if value, ok := zero.Get("a"); ok || value != nil {
		t.Errorf("Get on a zero-value table = %#v, %v; want nil, false", value, ok)
	}
	if got := zero.Interface(); len(got.(map[string]any)) != 0 {
		t.Errorf("Interface() on a zero-value table = %#v; want an empty map", got)
	}
}

// TestTableStringWithUnsupportedKeyAndValue covers the two debug-rendering
// fallbacks: a key that is not one of the literal types, and a value that has
// no Lua rendering at all.
func TestTableStringWithUnsupportedKeyAndValue(t *testing.T) {
	tbl := &Table{entries: []Entry{
		{Key: nil, Value: int64(1)},
		{Key: "ok", Value: []any{int64(1), int64(2)}},
	}}

	want := `{[?]=1, ok=[1 2]}`
	if got := tbl.String(); got != want {
		t.Fatalf("String() = %q; want %q", got, want)
	}
}

func TestKeyToStringFallback(t *testing.T) {
	if got := keyToString([]int{1, 2}); got != "[1 2]" {
		t.Fatalf("keyToString([]int{1, 2}) = %q; want %q", got, "[1 2]")
	}
}

func TestNormalizeKeyUintOverflow(t *testing.T) {
	u := ^uint(0) // the largest value of the platform's uint type
	_, ok := normalizeKey(u)
	wantOK := uint64(u) <= math.MaxInt64
	if ok != wantOK {
		t.Fatalf("normalizeKey(%d) ok = %v; want %v", u, ok, wantOK)
	}
}

func TestMarshalDepthErrorsForMapAndTable(t *testing.T) {
	e := Encoder{MaxDepth: 1}
	cases := []struct {
		name string
		v    any
	}{
		{"map nested in an array", []any{map[string]any{"a": int64(1)}}},
		{"rich table nested in an array", []any{MustParseTable(`{a = 1}`)}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := e.Marshal(tc.v)
			if err == nil {
				t.Fatal("expecting a depth error")
			}

			var ee *EncodeError
			if !errors.As(err, &ee) {
				t.Fatalf("error %T is not an *EncodeError: %s", err, err)
			}
			if !strings.Contains(ee.Msg, "nesting depth exceeds the maximum of 1") {
				t.Fatalf("unexpected message: %s", ee.Msg)
			}
			if ee.Path != "[0]" {
				t.Fatalf("unexpected path %q; want [0]", ee.Path)
			}
		})
	}
}

// TestMarshalRejectsUnsupportedTableKey covers the encoder's key validation
// for a *Table that was built by hand: a Go value outside the key domain is
// reported with the path of its field.
func TestMarshalRejectsUnsupportedTableKey(t *testing.T) {
	bad := &Table{entries: []Entry{{Key: []int{1}, Value: int64(2)}}}

	_, err := Marshal(map[string]any{"t": bad})
	if err == nil {
		t.Fatal("expecting an error")
	}

	var ee *EncodeError
	if !errors.As(err, &ee) {
		t.Fatalf("error %T is not an *EncodeError: %s", err, err)
	}
	if !strings.Contains(ee.Msg, "unsupported table key type []int") {
		t.Fatalf("unexpected message: %s", ee.Msg)
	}
	if ee.Path != ".t" {
		t.Fatalf("unexpected path %q; want .t", ee.Path)
	}
}

// TestMarshalRejectsNonFiniteTableKey covers the key half of the float
// rejection: NaN and infinities cannot be written even in bracket form.
func TestMarshalRejectsNonFiniteTableKey(t *testing.T) {
	for _, key := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		tbl := &Table{entries: []Entry{{Key: key, Value: int64(1)}}}

		_, err := Marshal(tbl)
		if err == nil {
			t.Fatalf("expecting an error for the key %v", key)
		}

		var ee *EncodeError
		if !errors.As(err, &ee) {
			t.Fatalf("error %T is not an *EncodeError: %s", err, err)
		}
		if !strings.Contains(ee.Msg, "cannot be represented") {
			t.Fatalf("unexpected message for %v: %s", key, ee.Msg)
		}
	}
}
