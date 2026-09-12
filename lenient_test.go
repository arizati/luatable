package luatable

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// skippedAt returns the Skipped value stored under key.
func skippedAt(t *testing.T, tbl *Table, key any) Skipped {
	t.Helper()
	value, ok := tbl.Get(key)
	if !ok {
		t.Fatalf("no entry for key %v", key)
	}
	skipped, ok := value.(Skipped)
	if !ok {
		t.Fatalf("entry %v is %T; want Skipped", key, value)
	}
	return skipped
}

func TestLenientSkipsExpressions(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"binary operator", `{a = 1 + 2, b = 3}`, "1 + 2"},
		{"concatenation", `{a = (1) .. "x", b = 3}`, `(1) .. "x"`},
		{"comparison", `{a = 1 < 2, b = 3}`, "1 < 2"},
		{"length operator", `{a = #t, b = 3}`, "#t"},
		{"dotted name", `{a = string.format, b = 3}`, "string.format"},
		{"call", `{a = loadstring("x"), b = 3}`, `loadstring("x")`},
		{"bare identifier", `{a = inf, b = 3}`, "inf"},
		{"nested call", `{a = f(g(1)), b = 3}`, "f(g(1))"},
		{"parenthesized call", `{a = (f()), b = 3}`, "(f())"},
		{"unary minus on a call", `{a = -f(), b = 3}`, "-f()"},
		{"function literal", `{a = function() return 1, 2 end, b = 3}`, "function() return 1, 2 end"},
		{"nested block in a function", `{a = function() if x then return {1} end end, b = 3}`,
			"function() if x then return {1} end end"},
		{"vararg", `{a = ..., b = 3}`, "..."},
		{"trailing comment", "{a = f() -- note\n, b = 3}", "f() -- note"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var p Parser
			p.Lenient = true
			tbl, err := p.ParseTable(tc.src)
			if err != nil {
				t.Fatalf("unexpected error: %s", err)
			}

			skipped := skippedAt(t, tbl, "a")
			if skipped.Text != tc.want {
				t.Fatalf("Skipped.Text = %q; want %q", skipped.Text, tc.want)
			}
			if skipped.Offset <= 0 {
				t.Fatalf("Skipped.Offset = %d; want the offset of the value", skipped.Offset)
			}
			if got, _ := tbl.Get("b"); got != int64(3) {
				t.Fatalf("b = %#v; want 3", got)
			}
		})
	}
}

func TestLenientKeepsPositionalFields(t *testing.T) {
	var p Parser
	p.Lenient = true
	tbl, err := p.ParseTable(`{1, f(), 3}`)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if !tbl.IsArray() {
		t.Fatalf("IsArray() = false; a skipped positional field must still occupy its index")
	}

	got := tbl.Array()
	if len(got) != 3 {
		t.Fatalf("len = %d; want 3", len(got))
	}
	if got[0] != int64(1) || got[2] != int64(3) {
		t.Fatalf("array = %#v; want 1 and 3 around the skipped value", got)
	}
	skipped, ok := got[1].(Skipped)
	if !ok {
		t.Fatalf("got[1] is %T; want Skipped", got[1])
	}
	if skipped.Text != "f()" {
		t.Fatalf("got[1].Text = %q; want %q", skipped.Text, "f()")
	}
}

func TestLenientDropsFieldsWithUndecodableKeys(t *testing.T) {
	srcs := []string{
		`{[{1,2}] = 3, ok = 1}`, // a table key
		`{[f()] = 3, ok = 1}`,   // an expression key
		`{[math.huge] = 3, ok = 1}`,
		`{[nil] = 3, ok = 1}`,
		`{[1 + 2] = 3, ok = 1}`,      // an expression that starts like a literal
		`{["a" .. "b"] = 1, ok = 1}`, // likewise for a concatenation
	}

	for _, src := range srcs {
		t.Run(src, func(t *testing.T) {
			var p Parser
			p.Lenient = true
			tbl, err := p.ParseTable(src)
			if err != nil {
				t.Fatalf("unexpected error: %s", err)
			}
			if tbl.Len() != 1 {
				t.Fatalf("Len() = %d; want 1: the field itself is dropped", tbl.Len())
			}
			if got, _ := tbl.Get("ok"); got != int64(1) {
				t.Fatalf("ok = %#v; want 1", got)
			}
		})
	}
}

func TestLenientKeepsDecodableValues(t *testing.T) {
	srcs := []string{
		`{1, 2.5, "three", true, nil}`,
		`{a = 1, b = {c = "x", d = {1, 2}}}`,
		`{["end"] = 1, [1] = "one", [true] = false}`,
		`{0x1p4, -1.5, (2)}`,
	}

	for _, src := range srcs {
		t.Run(src, func(t *testing.T) {
			want, err := Parse(src)
			if err != nil {
				t.Fatalf("strict parse failed: %s", err)
			}
			var p Parser
			p.Lenient = true
			got, err := p.Parse(src)
			if err != nil {
				t.Fatalf("lenient parse failed: %s", err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("lenient result %#v differs from the strict result %#v", got, want)
			}
		})
	}
}

func TestLenientNestedTables(t *testing.T) {
	var p Parser
	p.Lenient = true
	tbl, err := p.ParseTable(`{srv = {port = f(), host = "h"}, list = {1, g(), 3}}`)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	srv, ok := tbl.Get("srv")
	if !ok {
		t.Fatal("no entry for srv")
	}
	inner, ok := srv.(*Table)
	if !ok {
		t.Fatalf("srv is %T; want *Table", srv)
	}
	if skipped := skippedAt(t, inner, "port"); skipped.Text != "f()" {
		t.Fatalf("srv.port.Text = %q; want %q", skipped.Text, "f()")
	}
	if got, _ := inner.Get("host"); got != "h" {
		t.Fatalf("srv.host = %#v; want \"h\"", got)
	}

	list, ok := tbl.Get("list")
	if !ok {
		t.Fatal("no entry for list")
	}
	array, ok := list.(*Table)
	if !ok {
		t.Fatalf("list is %T; want *Table", list)
	}
	if !array.IsArray() || array.Len() != 3 {
		t.Fatalf("list must stay an array of 3 elements; Len() = %d", array.Len())
	}
}

func TestLenientDataDumperShape(t *testing.T) {
	// The shape DataDumper produces for a table that contains a C function, a
	// dumped function and the numbers it renders without quoting.
	src := `return {builtin=string.format, fn=loadstring("\27LJ\2\8"), huge=inf, nan=nan, id=1}`

	var p Parser
	p.Lenient = true
	p.AllowReturnPrefix = true
	tbl, err := p.ParseTable(src)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	for _, key := range []string{"builtin", "fn", "huge", "nan"} {
		if skipped := skippedAt(t, tbl, key); skipped.Text == "" {
			t.Fatalf("%s: empty Skipped.Text", key)
		}
	}
	if got, _ := tbl.Get("id"); got != int64(1) {
		t.Fatalf("id = %#v; want 1", got)
	}
}

func TestLenientKeepsStructuralErrors(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"missing value", `{a = }`},
		{"missing value before a separator", `{a = , b = 1}`},
		{"unterminated call", `{a = f(`},
		{"unterminated parenthesis", `{a = (f()`},
		{"unterminated key", `{[f() = 1`},
		{"unterminated string", `{a = f("x}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var p Parser
			p.Lenient = true
			if _, err := p.ParseTable(tc.src); err == nil {
				t.Fatal("expecting an error even in lenient mode")
			}
		})
	}
}

// TestLenientReportsLexicalErrorsInsideSkippedValues checks that skipping a value
// does not replace the lexical error that ended it with the skipper's own "end
// of input" message: the reason and the offset of an unterminated string or long
// bracket have to survive, even when the value is a nested constructor that the
// outer field ends up skipping.
func TestLenientReportsLexicalErrorsInsideSkippedValues(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"string in a nested table", `{a = {1, "abc}}`, "unfinished string literal"},
		{"long bracket in a nested table", `{a = {[==[x}}`, "unfinished long bracket"},
		{"string behind a unary minus", `{a = -"abc}`, "unfinished string literal"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var p Parser
			p.Lenient = true

			_, err := p.ParseTable(tc.src)
			if err == nil {
				t.Fatal("expecting an error even in lenient mode")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %v does not mention %q", err, tc.want)
			}
		})
	}
}

func TestLenientSkipsPositionalExpressions(t *testing.T) {
	var p Parser
	p.Lenient = true
	tbl, err := p.ParseTable(`{1, f(), function() return 1, 2 end, 4}`)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if !tbl.IsArray() || tbl.Len() != 4 {
		t.Fatalf("the table must stay an array of 4 elements; Len() = %d", tbl.Len())
	}

	for i, want := range []string{"", "f()", "function() return 1, 2 end", ""} {
		value, ok := tbl.Get(int64(i + 1))
		if !ok {
			t.Fatalf("no entry for index %d", i+1)
		}
		if want == "" {
			if _, isSkipped := value.(Skipped); isSkipped {
				t.Fatalf("index %d was skipped; want a literal", i+1)
			}
			continue
		}
		skipped, ok := value.(Skipped)
		if !ok {
			t.Fatalf("index %d is %T; want Skipped", i+1, value)
		}
		if skipped.Text != want {
			t.Fatalf("index %d: Skipped.Text = %q; want %q", i+1, skipped.Text, want)
		}
	}
}

func TestLenientStopsAtMaxDepth(t *testing.T) {
	deep := strings.Repeat("{", 400) + "1" + strings.Repeat("}", 400)
	if _, err := Parse(deep); err == nil {
		t.Fatal("the strict parser must reject nesting beyond MaxDepth")
	}

	// Lenient recovery stops where the depth limit is reached: the value that
	// would exceed it is skipped, and the levels above stay decoded. Skipping
	// is iterative, so the difference in depth cannot exhaust the stack.
	var p Parser
	p.Lenient = true
	tbl, err := p.ParseTable(deep)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	level := 0
	for {
		value, ok := tbl.Get(int64(1))
		if !ok {
			t.Fatalf("no entry at level %d", level)
		}
		skipped, isSkipped := value.(Skipped)
		if isSkipped {
			t.Logf("recovery stopped at level %d with %q", level, truncate(skipped.Text, 24))
			if !strings.HasPrefix(skipped.Text, "{") || !strings.HasSuffix(skipped.Text, "}") {
				t.Fatalf("terminal Skipped.Text = %q; want the remaining nested value", truncate(skipped.Text, 32))
			}
			return
		}
		nested, ok := value.(*Table)
		if !ok {
			t.Fatalf("value at level %d is %T; want *Table or Skipped", level, value)
		}
		level++
		if level > DefaultMaxDepth {
			t.Fatalf("nesting beyond MaxDepth %d survived the lenient parse", DefaultMaxDepth)
		}
		tbl = nested
	}
}

func TestMarshalRejectsSkipped(t *testing.T) {
	cases := []struct {
		name string
		v    any
	}{
		{"rich table", MustParseTableLenient(`{fn = f()}`)},
		{"generic map", map[string]any{"fn": Skipped{Text: "f()"}}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Marshal(tc.v)
			var encodeErr *EncodeError
			if !errors.As(err, &encodeErr) {
				t.Fatalf("error %v is not an *EncodeError", err)
			}
			if encodeErr.Path != ".fn" {
				t.Fatalf("path = %q; want %q", encodeErr.Path, ".fn")
			}
			if !strings.Contains(encodeErr.Msg, "skipped") {
				t.Fatalf("message %q does not mention the skipped value", encodeErr.Msg)
			}
		})
	}
}

func TestEmitSkipped(t *testing.T) {
	src := `{id = 1, fn = loadstring("x"), nested = {f = f(), ok = true}}`
	var p Parser
	p.Lenient = true
	tbl, err := p.ParseTable(src)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	// The raw text is not written back unless the switch asks for it.
	if _, err := Marshal(tbl); err == nil {
		t.Fatal("Marshal must reject a Skipped value by default")
	}

	e := Encoder{EmitSkipped: true}
	out, err := e.Marshal(tbl)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	want := `{id = 1,fn = loadstring("x"),nested = {f = f(),ok = true}}`
	if string(out) != want {
		t.Fatalf("output = %s; want %s", out, want)
	}

	// The emitted text goes back through the lenient parser with the same
	// data. Skipped.Offset is positional metadata, so it belongs to the text
	// that was parsed and is not compared here.
	var round Parser
	round.Lenient = true
	back, err := round.ParseTable(string(out))
	if err != nil {
		t.Fatalf("the emitted text is not parseable: %s", err)
	}
	if got, _ := back.Get("id"); got != int64(1) {
		t.Fatalf("id = %#v; want 1", got)
	}
	if got := skippedAt(t, back, "fn").Text; got != `loadstring("x")` {
		t.Fatalf("fn.Text = %q; want %q", got, `loadstring("x")`)
	}
	nested, ok := back.Get("nested")
	if !ok {
		t.Fatal("no entry for nested")
	}
	inner, ok := nested.(*Table)
	if !ok {
		t.Fatalf("nested is %T; want *Table", nested)
	}
	if got := skippedAt(t, inner, "f").Text; got != "f()" {
		t.Fatalf("nested.f.Text = %q; want %q", got, "f()")
	}
	if got, _ := inner.Get("ok"); got != true {
		t.Fatalf("nested.ok = %#v; want true", got)
	}
}

func TestEmitSkippedBuildsSpecialTables(t *testing.T) {
	// The switch is also the way to hand-build a table that holds something
	// this package cannot decode.
	e := Encoder{EmitSkipped: true}
	out, err := e.Marshal(map[string]any{
		"fn":   Skipped{Text: "function() return 42 end"},
		"huge": Skipped{Text: "math.huge"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	want := `{fn = function() return 42 end,huge = math.huge}`
	if string(out) != want {
		t.Fatalf("output = %s; want %s", out, want)
	}

	if _, err := Parse(string(out)); err == nil {
		t.Fatal("the emitted text must still be rejected by the strict parser")
	}
}

func TestEmitSkippedRejectsEmptyText(t *testing.T) {
	e := Encoder{EmitSkipped: true}
	_, err := e.Marshal(map[string]any{"a": Skipped{}})

	var encodeErr *EncodeError
	if !errors.As(err, &encodeErr) {
		t.Fatalf("error %v is not an *EncodeError", err)
	}
	if encodeErr.Path != ".a" {
		t.Fatalf("path = %q; want %q", encodeErr.Path, ".a")
	}
}

// MustParseTableLenient parses src with Parser.Lenient set, for tests.
func MustParseTableLenient(src string) *Table {
	var p Parser
	p.Lenient = true
	tbl, err := p.ParseTable(src)
	if err != nil {
		panic(err)
	}
	return tbl
}
