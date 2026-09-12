package luatable

import (
	"strings"
	"testing"
)

func TestDecodeStringLiteral(t *testing.T) {
	t.Run("short strings", func(t *testing.T) {
		cases := []struct {
			src  string
			want string
		}{
			{`"hello"`, "hello"},
			{`'hello'`, "hello"},
			{`""`, ""},
			{`''`, ""},
			{`"a\tb"`, "a\tb"},
			{`"a\nb"`, "a\nb"},
			{`"a\rb"`, "a\rb"},
			{`"\a\b\f\v"`, "\a\b\f\v"},
			{`"\\"`, `\`},
			{`"\""`, `"`},
			{`'\''`, `'`},
			{`"\65\66\67"`, "ABC"},
			{`"\x41\x42"`, "AB"},
			{`"\u{48}\u{49}"`, "HI"},
			{`"\u{1F600}"`, "\U0001F600"},
			{`"\u{10FFFF}"`, "\U0010FFFF"},
			{`"caf\u{e9}"`, "café"},
			{`"tab\tseparated"`, "tab\tseparated"},
			{`"no escapes"`, "no escapes"},
		}

		for _, tc := range cases {
			t.Run(tc.src, func(t *testing.T) {
				got, offset, err := decodeStringLiteral(tc.src)
				if err != nil {
					t.Fatalf("unexpected error at offset %d: %s", offset, err)
				}
				if got != tc.want {
					t.Fatalf("unexpected value; got %q; want %q", got, tc.want)
				}
			})
		}
	})

	t.Run("z escape skips whitespace", func(t *testing.T) {
		// The escapes are built with an interpreted string so that the
		// whitespace skipped by \z is real whitespace.
		src := "\"a\\z   \n\t  b\""
		got, _, err := decodeStringLiteral(src)
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
		if got != "ab" {
			t.Fatalf("unexpected value; got %q; want %q", got, "ab")
		}
	})

	t.Run("line continuations", func(t *testing.T) {
		cases := []struct {
			name string
			raw  string
			want string
		}{
			{"LF", "\"a\\\nb\"", "a\nb"},
			{"CR", "\"a\\\rb\"", "a\nb"},
			{"CRLF", "\"a\\\r\nb\"", "a\nb"},
			{"LFCR", "\"a\\\n\rb\"", "a\nb"},
			{"two continuations", "\"a\\\n\\\nb\"", "a\n\nb"},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				got, _, err := decodeStringLiteral(tc.raw)
				if err != nil {
					t.Fatalf("unexpected error: %s", err)
				}
				if got != tc.want {
					t.Fatalf("decoded value = %q; want %q", got, tc.want)
				}
			})
		}
	})

	t.Run("long strings", func(t *testing.T) {
		cases := []struct {
			src  string
			want string
		}{
			{`[[abc]]`, "abc"},
			{"[[abc]]", "abc"},
			{"[[\nabc]]", "abc"},
			{"[[\r\nabc]]", "abc"},
			{"[[\rabc]]", "abc"},
			{`[=[a]]b]=]`, "a]]b"},
			{`[==[]==]`, ""},
			{`[==[a]=]b]==]`, "a]=]b"},
			{"[[ first line\nsecond line]]", " first line\nsecond line"},
			{"[[a\n]]", "a\n"},
		}

		for _, tc := range cases {
			t.Run(tc.src, func(t *testing.T) {
				got, offset, err := decodeStringLiteral(tc.src)
				if err != nil {
					t.Fatalf("unexpected error at offset %d: %s", offset, err)
				}
				if got != tc.want {
					t.Fatalf("unexpected value; got %q; want %q", got, tc.want)
				}
			})
		}
	})

	t.Run("errors", func(t *testing.T) {
		cases := []struct {
			src     string
			wantMsg string
		}{
			{`"\q"`, "invalid escape sequence"},
			{`"\x1"`, "hexadecimal digit expected"},
			{`"\xZZ"`, "hexadecimal digit expected"},
			{`"\256"`, "decimal escape sequence too large"},
			{`"\u{}"`, "invalid '\\u{...}' escape"},
			{`"\u{ZZ}"`, "invalid '\\u{...}' escape"},
			{`"\u{110000}"`, "invalid Unicode code point"},
			{`"\u{D800}"`, "invalid Unicode code point"},
			{`"\u{DFFF}"`, "invalid Unicode code point"},
			{`"\u48"`, "missing '{' in '\\u{...}' escape"},
			{`"\u{12"`, "missing '}' in '\\u{...}' escape"},
			{`"abc\"`, "unfinished escape sequence"},
		}

		for _, tc := range cases {
			t.Run(tc.src, func(t *testing.T) {
				_, _, err := decodeStringLiteral(tc.src)
				if err == nil {
					t.Fatalf("expecting an error for %q", tc.src)
				}
				if !strings.Contains(err.Error(), tc.wantMsg) {
					t.Fatalf("unexpected error %q; want it to contain %q", err, tc.wantMsg)
				}
			})
		}
	})

	t.Run("error offset points at the escape", func(t *testing.T) {
		// raw: " a b \ q "
		// index: 0 1 2 3 4 5
		src := `"ab\q"`
		_, offset, err := decodeStringLiteral(src)
		if err == nil {
			t.Fatal("expecting an error")
		}
		if offset != 3 {
			t.Fatalf("unexpected error offset; got %d; want 3", offset)
		}
	})

	t.Run("empty literal", func(t *testing.T) {
		if _, _, err := decodeStringLiteral(""); err == nil {
			t.Fatal("expecting an error for an empty literal")
		}
	})
}

// TestZEscapeSkipsLineBreaks covers the "\z" escape end to end. The escape
// skips every kind of Lua whitespace, line breaks included: "x\z\n y" is "xy"
// on Lua 5.2 and later and on LuaJIT, while Lua 5.1 rejects the escape itself.
// The scanner has to consume the whitespace instead of ending the string at
// the newline, and the decoder then drops the same span.
func TestZEscapeSkipsLineBreaks(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"line feed", "{a = \"x\\z\ny\"}", "xy"},
		{"carriage return", "{a = \"x\\z\ry\"}", "xy"},
		{"carriage return line feed", "{a = \"x\\z\r\ny\"}", "xy"},
		{"several line breaks", "{a = \"x\\z\n\n\r\n  y\"}", "xy"},
		{"spaces and tabs", "{a = \"x\\z \t y\"}", "xy"},
		{"vertical tab and form feed", "{a = \"x\\z\v\f y\"}", "xy"},
		{"up to the closing quote", "{a = \"x\\z \n \"}", "x"},
		{"before another escape", "{a = \"x\\z \\n y\"}", "x\n y"},
		{"without whitespace", "{a = \"x\\zq\"}", "xq"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Parse(tc.src)
			if err != nil {
				t.Fatalf("unexpected error: %s", err)
			}
			if value := got.(map[string]any)["a"]; value != tc.want {
				t.Fatalf("value = %q; want %q", value, tc.want)
			}
		})
	}

	t.Run("unterminated", func(t *testing.T) {
		for _, src := range []string{"{a = \"x\\z", "{a = \"x\\z   "} {
			_, err := Parse(src)
			if err == nil {
				t.Fatalf("expecting an error for %q", src)
			}
			if !strings.Contains(err.Error(), "unfinished string literal") {
				t.Fatalf("unexpected error for %q: %s", src, err)
			}
		}
	})
}

// TestShortStringLineContinuations pins the parser-level semantics of the
// backslash-newline escape. LF, CR, CRLF and LFCR each continue the string
// with a single newline in the value, as Lua reads them, and two separate
// escapes produce two newlines.
//
// A second raw newline after one backslash is an unterminated string, because
// only the directly escaped newline is consumed: Lua itself rejects
// "a\<LF><LF>b" for the same reason.
func TestShortStringLineContinuations(t *testing.T) {
	accepted := []struct {
		name string
		src  string
		want string
	}{
		{"LF", "{a = \"x\\\ny\"}", "x\ny"},
		{"CR", "{a = \"x\\\ry\"}", "x\ny"},
		{"CRLF", "{a = \"x\\\r\ny\"}", "x\ny"},
		{"LFCR", "{a = \"x\\\n\ry\"}", "x\ny"},
		{"two continuations", "{a = \"x\\\n\\\ny\"}", "x\n\ny"},
	}

	for _, tc := range accepted {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Parse(tc.src)
			if err != nil {
				t.Fatalf("unexpected error: %s", err)
			}
			if value := got.(map[string]any)["a"]; value != tc.want {
				t.Fatalf("value = %q; want %q", value, tc.want)
			}
		})
	}

	rejected := []struct {
		name string
		src  string
	}{
		{"two line feeds after one backslash", "{a = \"x\\\n\ny\"}"},
		{"two carriage returns after one backslash", "{a = \"x\\\r\ry\"}"},
		{"line feed then a carriage return line feed", "{a = \"x\\\n\r\ny\"}"},
	}

	for _, tc := range rejected {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(tc.src)
			if err == nil {
				t.Fatalf("expecting an error for %q", tc.src)
			}
			if !strings.Contains(err.Error(), "unfinished string literal") {
				t.Fatalf("unexpected error for %q: %s", tc.src, err)
			}
		})
	}
}
