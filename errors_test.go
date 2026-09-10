package luatable

import (
	"errors"
	"strings"
	"testing"
)

func TestParseErrors(t *testing.T) {
	cases := []struct {
		name     string
		src      string
		wantMsg  string
		wantLine int
		wantCol  int
	}{
		{
			name:     "empty input",
			src:      "",
			wantMsg:  "expected table constructor '{', found end of input",
			wantLine: 1,
			wantCol:  1,
		},
		{
			name:     "bare scalar instead of a table",
			src:      "1",
			wantMsg:  `expected table constructor '{', found number literal "1"`,
			wantLine: 1,
			wantCol:  1,
		},
		{
			name:     "bare bracket instead of a table",
			src:      "[1]",
			wantMsg:  "expected table constructor '{', found '['",
			wantLine: 1,
			wantCol:  1,
		},
		{
			name:     "return prefix disabled",
			src:      "return {1}",
			wantMsg:  `expected table constructor '{', found "return"`,
			wantLine: 1,
			wantCol:  1,
		},
		{
			name:     "unterminated table",
			src:      "{",
			wantMsg:  "unexpected end of input, expected '}'",
			wantLine: 1,
			wantCol:  2,
		},
		{
			name:     "unterminated table with fields",
			src:      "{[1] = 1",
			wantMsg:  "unexpected end of input, expected '}'",
			wantLine: 1,
			wantCol:  9,
		},
		{
			name:     "missing separator",
			src:      "{1 2}",
			wantMsg:  `expected ',' or '}' after table field, found number literal "2"`,
			wantLine: 1,
			wantCol:  4,
		},
		{
			name:     "bare identifier as a field",
			src:      "{a 1}",
			wantMsg:  `unsupported expression "a"`,
			wantLine: 1,
			wantCol:  2,
		},
		{
			name:     "bare identifier as a value",
			src:      "{a = math.huge}",
			wantMsg:  `unsupported expression "math"`,
			wantLine: 1,
			wantCol:  6,
		},
		{
			name:     "missing equals after bracket key",
			src:      "{[1] 2}",
			wantMsg:  `expected '=' after the table key, found number literal "2"`,
			wantLine: 1,
			wantCol:  6,
		},
		{
			name:     "missing closing bracket",
			src:      "{[1",
			wantMsg:  "expected ']' to close the table key, found end of input",
			wantLine: 1,
			wantCol:  4,
		},
		{
			name:     "nil table key",
			src:      "{[nil] = 1}",
			wantMsg:  "table index is nil",
			wantLine: 1,
			wantCol:  2,
		},
		{
			name:     "table as table key",
			src:      "{[{}] = 1}",
			wantMsg:  "unsupported table key type *luatable.Table",
			wantLine: 1,
			wantCol:  2,
		},
		{
			name:     "infinite table key",
			src:      "{[0x1p1024] = 1}",
			wantMsg:  "table key is NaN or infinite",
			wantLine: 1,
			wantCol:  2,
		},
		{
			name:     "unfinished short string",
			src:      `{"abc}`,
			wantMsg:  "unfinished string literal",
			wantLine: 1,
			wantCol:  2,
		},
		{
			name:     "short string ending in backslash",
			src:      `{"abc\`,
			wantMsg:  "unfinished string literal",
			wantLine: 1,
			wantCol:  2,
		},
		{
			name:     "unfinished long string",
			src:      "{a = [[x}",
			wantMsg:  "unfinished long bracket",
			wantLine: 1,
			wantCol:  6,
		},
		{
			name:     "unfinished long comment",
			src:      "{--[[ x }",
			wantMsg:  "unfinished long bracket",
			wantLine: 1,
			wantCol:  4,
		},
		{
			name:     "missing value after equals",
			src:      "{a=}",
			wantMsg:  "unexpected '}', expected a value",
			wantLine: 1,
			wantCol:  4,
		},
		{
			name:     "missing operand after unary minus",
			src:      "{-}",
			wantMsg:  "unexpected '}', expected a value",
			wantLine: 1,
			wantCol:  3,
		},
		{
			name:     "unary minus on a table",
			src:      "{-{}}",
			wantMsg:  "unary '-' requires a number operand",
			wantLine: 1,
			wantCol:  2,
		},
		{
			name:     "unsupported operator",
			src:      "{a=1+2}",
			wantMsg:  "unexpected character '+'",
			wantLine: 1,
			wantCol:  5,
		},
		{
			name:     "leading comma",
			src:      "{,}",
			wantMsg:  "unexpected ',', expected a value",
			wantLine: 1,
			wantCol:  2,
		},
		{
			name:     "trailing garbage",
			src:      "{}extra",
			wantMsg:  `unexpected identifier "extra" after table constructor`,
			wantLine: 1,
			wantCol:  3,
		},
		{
			name:     "two tables",
			src:      "{1}{2}",
			wantMsg:  "unexpected '{' after table constructor",
			wantLine: 1,
			wantCol:  4,
		},
		{
			name:     "missing closing parenthesis",
			src:      "{(1}",
			wantMsg:  "expected ')', found '}'",
			wantLine: 1,
			wantCol:  4,
		},
		{
			name:     "error position on a later line",
			src:      "{\n  a = 1,\n  b = ,\n}",
			wantMsg:  "unexpected ',', expected a value",
			wantLine: 3,
			wantCol:  7,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(tc.src)
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
			if se.Line != tc.wantLine || se.Column != tc.wantCol {
				t.Fatalf("unexpected position; got line %d, column %d; want line %d, column %d",
					se.Line, se.Column, tc.wantLine, tc.wantCol)
			}
			if se.Offset < 0 || se.Offset > len(tc.src) {
				t.Fatalf("offset %d is out of range for input of length %d", se.Offset, len(tc.src))
			}
		})
	}
}

func TestParseRespectsMaxDepthForExpressions(t *testing.T) {
	p := &Parser{MaxDepth: 5}

	if _, err := p.Parse("{- - - -1}"); err != nil {
		t.Fatalf("unexpected error within the depth limit: %s", err)
	}
	if _, err := p.Parse("{(1)}"); err != nil {
		t.Fatalf("unexpected error within the depth limit: %s", err)
	}

	sources := []string{
		"{" + strings.Repeat("(", 50) + "1" + strings.Repeat(")", 50) + "}",
		"{" + strings.Repeat("- ", 50) + "1}",
	}
	for _, src := range sources {
		_, err := p.Parse(src)
		if err == nil {
			t.Fatalf("expecting a depth error for a deep expression chain")
		}

		var se *SyntaxError
		if !errors.As(err, &se) {
			t.Fatalf("unexpected error type %T", err)
		}
		if !strings.Contains(se.Msg, "nesting depth exceeds") {
			t.Fatalf("unexpected error message: %s", se.Msg)
		}
	}
}

func TestSyntaxErrorFormat(t *testing.T) {
	se := &SyntaxError{Msg: "boom", Offset: 3, Line: 2, Column: 4}
	want := "luatable: boom at line 2, column 4 (offset 3)"
	if got := se.Error(); got != want {
		t.Fatalf("unexpected Error() string; got %q; want %q", got, want)
	}
}

func TestSyntaxErrorOffsetMatchesPosition(t *testing.T) {
	_, err := Parse("{\n  a = ,\n}")
	if err == nil {
		t.Fatal("expecting an error")
	}

	var se *SyntaxError
	if !errors.As(err, &se) {
		t.Fatalf("unexpected error type %T", err)
	}

	line, column := positionAt("{\n  a = ,\n}", se.Offset)
	if line != se.Line || column != se.Column {
		t.Fatalf("offset %d maps to line %d column %d but SyntaxError reports line %d column %d",
			se.Offset, line, column, se.Line, se.Column)
	}
}

func TestPositionAt(t *testing.T) {
	src := "ab\ncd\nef"

	cases := []struct {
		offset   int
		wantLine int
		wantCol  int
	}{
		{-5, 1, 1}, // clamped
		{0, 1, 1},
		{1, 1, 2},
		{2, 1, 3},
		{3, 2, 1},
		{4, 2, 2},
		{6, 3, 1},
		{8, 3, 3},
		{100, 3, 3}, // clamped
	}

	for _, tc := range cases {
		line, column := positionAt(src, tc.offset)
		if line != tc.wantLine || column != tc.wantCol {
			t.Fatalf("positionAt(%d) = (line %d, column %d); want (line %d, column %d)",
				tc.offset, line, column, tc.wantLine, tc.wantCol)
		}
	}
}

func TestParseErrorForUnsupportedKeyTypeReportsTableType(t *testing.T) {
	_, err := Parse("{[{}] = 1}")
	if err == nil {
		t.Fatal("expecting an error")
	}
	if !strings.Contains(err.Error(), "*luatable.Table") {
		t.Fatalf("unexpected error message: %s", err)
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("hello", 10); got != "hello" {
		t.Fatalf("truncate should not shorten short strings; got %q", got)
	}
	if got := truncate("hello world", 5); got != "hello..." {
		t.Fatalf("unexpected truncate result: %q", got)
	}
}
