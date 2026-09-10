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

	t.Run("line continuation", func(t *testing.T) {
		src := "\"a\\\nb\""
		got, _, err := decodeStringLiteral(src)
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
		if got != "a\nb" {
			t.Fatalf("unexpected value; got %q; want %q", got, "a\nb")
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
