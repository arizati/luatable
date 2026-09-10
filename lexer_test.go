package luatable

import (
	"strings"
	"testing"
)

// lexAll tokenizes src and returns every token up to (but excluding) EOF.
func lexAll(t *testing.T, src string) []token {
	t.Helper()

	l := &lexer{src: src}
	var out []token
	for {
		l.next()
		if l.err != nil {
			t.Fatalf("unexpected lexer error for %q: %s", src, l.err)
		}
		if l.tok.typ == tokenEOF {
			return out
		}
		out = append(out, l.tok)
	}
}

func TestLexerPunctuation(t *testing.T) {
	got := lexAll(t, "{ } [ ] = , ; ( ) -")
	want := []tokenType{
		tokenLBrace, tokenRBrace,
		tokenLBracket, tokenRBracket,
		tokenAssign, tokenComma, tokenSemicolon,
		tokenLParen, tokenRParen, tokenMinus,
	}

	if len(got) != len(want) {
		t.Fatalf("unexpected token count; got %d; want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].typ != want[i] {
			t.Fatalf("unexpected token %d; got %v; want %v", i, got[i].typ, want[i])
		}
	}
}

func TestLexerNamesAndKeywords(t *testing.T) {
	src := "foo _bar Baz1 nil true false return"
	got := lexAll(t, src)

	wantTypes := []tokenType{
		tokenName, tokenName, tokenName,
		tokenKeyword, tokenKeyword, tokenKeyword, tokenKeyword,
	}
	wantText := []string{"foo", "_bar", "Baz1", "nil", "true", "false", "return"}

	if len(got) != len(wantTypes) {
		t.Fatalf("unexpected token count; got %d; want %d", len(got), len(wantTypes))
	}
	for i := range wantTypes {
		if got[i].typ != wantTypes[i] {
			t.Fatalf("token %d has type %v; want %v", i, got[i].typ, wantTypes[i])
		}
		if got[i].text != wantText[i] {
			t.Fatalf("token %d has text %q; want %q", i, got[i].text, wantText[i])
		}
	}
}

func TestLexerNumbers(t *testing.T) {
	src := "1 2.5 .5 1. 1e10 1E-3 0xFF 0X10 0x1p4 0xA.8p-1"
	got := lexAll(t, src)

	want := []string{"1", "2.5", ".5", "1.", "1e10", "1E-3", "0xFF", "0X10", "0x1p4", "0xA.8p-1"}
	if len(got) != len(want) {
		t.Fatalf("unexpected token count; got %d; want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].typ != tokenNumber {
			t.Fatalf("token %d has type %v; want number", i, got[i].typ)
		}
		if got[i].text != want[i] {
			t.Fatalf("token %d has text %q; want %q", i, got[i].text, want[i])
		}
	}
}

func TestLexerStrings(t *testing.T) {
	src := `"a" 'b' [[c]] [=[d]=] [==[e]==]`
	got := lexAll(t, src)

	want := []string{`"a"`, `'b'`, `[[c]]`, `[=[d]=]`, `[==[e]==]`}
	if len(got) != len(want) {
		t.Fatalf("unexpected token count; got %d; want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].typ != tokenString {
			t.Fatalf("token %d has type %v; want string", i, got[i].typ)
		}
		if got[i].text != want[i] {
			t.Fatalf("token %d has text %q; want %q", i, got[i].text, want[i])
		}
	}
}

func TestLexerOffsets(t *testing.T) {
	got := lexAll(t, "{a=1}")
	wantTyp := []tokenType{tokenLBrace, tokenName, tokenAssign, tokenNumber, tokenRBrace}
	wantOff := []int{0, 1, 2, 3, 4}

	if len(got) != len(wantTyp) {
		t.Fatalf("unexpected token count; got %d; want %d", len(got), len(wantTyp))
	}
	for i := range wantTyp {
		if got[i].typ != wantTyp[i] || got[i].offset != wantOff[i] {
			t.Fatalf("token %d: got (%v, offset %d); want (%v, offset %d)",
				i, got[i].typ, got[i].offset, wantTyp[i], wantOff[i])
		}
	}
}

func TestLexerSkipsWhitespaceAndComments(t *testing.T) {
	src := "1 -- line comment\n2 --[[ block ]] 3 --[==[ long\ncomment ]==] 4"
	got := lexAll(t, src)

	want := []string{"1", "2", "3", "4"}
	if len(got) != len(want) {
		t.Fatalf("unexpected token count; got %d; want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].text != want[i] {
			t.Fatalf("token %d has text %q; want %q", i, got[i].text, want[i])
		}
	}
}

func TestLexerLineCommentEndsAtCarriageReturn(t *testing.T) {
	got := lexAll(t, "1 -- comment\r2")
	if len(got) != 2 || got[1].text != "2" {
		t.Fatalf("unexpected tokens: %#v", got)
	}
}

func TestLexerDashInsideTableIsNotAComment(t *testing.T) {
	// "--" inside an expression is still a comment, but a single '-' is not.
	got := lexAll(t, "{-1}")
	want := []tokenType{tokenLBrace, tokenMinus, tokenNumber, tokenRBrace}
	if len(got) != len(want) {
		t.Fatalf("unexpected token count; got %d; want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].typ != want[i] {
			t.Fatalf("token %d has type %v; want %v", i, got[i].typ, want[i])
		}
	}
}

func TestLexerBracketVersusLongString(t *testing.T) {
	// A '[' that is not followed by '[' or a run of '=' followed by '[' is a
	// plain left bracket, while "[[" introduces a long string.
	got := lexAll(t, "[a] = [[b]]")
	want := []tokenType{
		tokenLBracket, tokenName, tokenRBracket,
		tokenAssign,
		tokenString, // [[b]]
	}
	if len(got) != len(want) {
		t.Fatalf("unexpected token count; got %d; want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].typ != want[i] {
			t.Fatalf("token %d has type %v; want %v", i, got[i].typ, want[i])
		}
	}
}

func TestLexerErrors(t *testing.T) {
	cases := []struct {
		name       string
		src        string
		wantMsg    string
		wantOffset int
	}{
		{"unterminated double quote", `"abc`, "unfinished string literal", 0},
		{"unterminated single quote", `'abc`, "unfinished string literal", 0},
		{"newline in short string", "\"a\nb\"", "unfinished string literal", 0},
		{"unterminated long string", "[[abc", "unfinished long bracket", 0},
		{"unterminated leveled long string", "[==[abc", "unfinished long bracket", 0},
		{"unterminated long comment", "--[[abc", "unfinished long bracket", 2},
		{"unexpected punctuation", "@", "unexpected character '@'", 0},
		{"unexpected non-ascii rune", "€", "unexpected character '€'", 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l := &lexer{src: tc.src}
			l.next()
			if l.err == nil {
				t.Fatalf("expecting a lexer error for %q", tc.src)
			}
			if !strings.Contains(l.err.Msg, tc.wantMsg) {
				t.Fatalf("unexpected error message %q; want it to contain %q", l.err.Msg, tc.wantMsg)
			}
			if l.err.Offset != tc.wantOffset {
				t.Fatalf("unexpected error offset; got %d; want %d", l.err.Offset, tc.wantOffset)
			}
		})
	}
}

func TestLexerEscapedQuoteIsNotTerminator(t *testing.T) {
	got := lexAll(t, `"a\"b"`)
	if len(got) != 1 {
		t.Fatalf("unexpected token count; got %d; want 1", len(got))
	}
	if got[0].text != `"a\"b"` {
		t.Fatalf("unexpected raw text %q", got[0].text)
	}
}
