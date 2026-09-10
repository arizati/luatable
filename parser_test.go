package luatable

import (
	"reflect"
	"strings"
	"testing"
)

// parseGeneric is a test helper that parses src and fails the test on error.
func parseGeneric(t *testing.T, src string) any {
	t.Helper()

	v, err := Parse(src)
	if err != nil {
		t.Fatalf("unexpected error for %q: %s", src, err)
	}
	return v
}

func TestParseArrayTables(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want any
	}{
		{"empty", "{}", map[string]any{}},
		{"single integer", "{1}", []any{int64(1)}},
		{"mixed scalars", `{1, "two", true, false, nil}`, []any{int64(1), "two", true, false, nil}},
		{"nested arrays", "{{1, 2}, {3, 4}}", []any{
			[]any{int64(1), int64(2)},
			[]any{int64(3), int64(4)},
		}},
		{"trailing comma", "{1, 2,}", []any{int64(1), int64(2)}},
		{"semicolon separators", "{1; 2; 3;}", []any{int64(1), int64(2), int64(3)}},
		{"mixed separators", "{1, 2; 3}", []any{int64(1), int64(2), int64(3)}},
		{"floats", "{1.5, .5, 1.}", []any{1.5, 0.5, 1.0}},
		{"negative numbers", "{-1, -2.5, - 3}", []any{int64(-1), -2.5, int64(-3)}},
		{"parenthesized literals", "{(1), (2.5), ((3))}", []any{int64(1), 2.5, int64(3)}},
		{"hex numbers", "{0xFF, 0x1p4}", []any{int64(255), 16.0}},
		{"strings", `{"a", 'b', [[c]]}`, []any{"a", "b", "c"}},
		{"array with nil hole", "{1, nil, 3}", []any{int64(1), nil, int64(3)}},
		{"explicit array indices", "{[1] = 'a', [2] = 'b'}", []any{"a", "b"}},
		{"deeply nested arrays", "{{{1}}}", []any{[]any{[]any{int64(1)}}}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseGeneric(t, tc.src)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("unexpected value; got %#v; want %#v", got, tc.want)
			}
		})
	}
}

func TestParseHashTables(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want map[string]any
	}{
		{"identifier keys", `{a = 1, b = "x"}`, map[string]any{"a": int64(1), "b": "x"}},
		{"bracket string key", `{["k"] = 1}`, map[string]any{"k": int64(1)}},
		{"bracket long string key", `{[ [[k]] ] = 1}`, map[string]any{"k": int64(1)}},
		{"numeric key", `{[10] = "v"}`, map[string]any{"10": "v"}},
		{"negative numeric key", `{[-3] = "v"}`, map[string]any{"-3": "v"}},
		{"boolean keys", `{[true] = 1, [false] = 2}`, map[string]any{"true": int64(1), "false": int64(2)}},
		{"float key", `{[1.5] = "x"}`, map[string]any{"1.5": "x"}},
		{"integral float key normalizes", `{[2.0] = "x"}`, map[string]any{"2": "x"}},
		{"mixed array and hash", `{1, 2, x = 3}`, map[string]any{"1": int64(1), "2": int64(2), "x": int64(3)}},
		{"sparse numeric keys", `{[1] = "a", [3] = "c"}`, map[string]any{"1": "a", "3": "c"}},
		{"nested hash", `{a = {b = {c = 1}}}`, map[string]any{
			"a": map[string]any{"b": map[string]any{"c": int64(1)}},
		}},
		{"table values", `{list = {1, 2}, map = {k = "v"}}`, map[string]any{
			"list": []any{int64(1), int64(2)},
			"map":  map[string]any{"k": "v"},
		}},
		{"escaped string key", `{["a\nb"] = 1}`, map[string]any{"a\nb": int64(1)}},
		{"keyword-like identifier values are rejected elsewhere", `{a = "nil"}`, map[string]any{"a": "nil"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseGeneric(t, tc.src)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("unexpected value; got %#v; want %#v", got, tc.want)
			}
		})
	}
}

func TestParseCommentsAreIgnored(t *testing.T) {
	src := `{
		-- a line comment before any field
		a = 1, -- trailing line comment
		b = 2, --[[ inline block comment ]]
		--[==[
			a long comment containing } and , and ]]
		]==]
		c = 3, --[=[ another long comment ]=]
	}`
	want := map[string]any{"a": int64(1), "b": int64(2), "c": int64(3)}

	got := parseGeneric(t, src)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected value; got %#v; want %#v", got, want)
	}
}

func TestParseCommentOnlyTable(t *testing.T) {
	got := parseGeneric(t, "{ --[[ nothing here ]] }")
	if !reflect.DeepEqual(got, map[string]any{}) {
		t.Fatalf("unexpected value: %#v", got)
	}
}

func TestParseSurroundingWhitespaceAndComments(t *testing.T) {
	got := parseGeneric(t, "\n\t -- leading comment\n { 1, 2 }\n -- trailing comment\n")
	if !reflect.DeepEqual(got, []any{int64(1), int64(2)}) {
		t.Fatalf("unexpected value: %#v", got)
	}
}

func TestParseTrailingSemicolon(t *testing.T) {
	got := parseGeneric(t, "{1, 2};")
	if !reflect.DeepEqual(got, []any{int64(1), int64(2)}) {
		t.Fatalf("unexpected value: %#v", got)
	}
}

func TestParseLongStringValues(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"simple", "{a = [[hello]]}", "hello"},
		{"leading newline is dropped", "{a = [[\nhello]]}", "hello"},
		{"embedded newlines", "{a = [[line1\nline2]]}", "line1\nline2"},
		{"levelled", "{a = [==[x]=]y]==]}", "x]=]y"},
		{"no escape processing", `{a = [[a\tb]]}`, `a\tb`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseGeneric(t, tc.src).(map[string]any)
			if got["a"] != tc.want {
				t.Fatalf("unexpected value; got %q; want %q", got["a"], tc.want)
			}
		})
	}
}

func TestParseEmptyTableBranches(t *testing.T) {
	for _, src := range []string{"{}", "{ }", "{\n}", "{ -- comment\n }", "{;}"} {
		got, err := Parse(src)
		if err != nil {
			// "{;}" is not a valid Lua table; skip it.
			continue
		}
		if !reflect.DeepEqual(got, map[string]any{}) {
			t.Fatalf("unexpected value for %q: %#v", src, got)
		}
	}
}

func TestParseReturnPrefix(t *testing.T) {
	src := `return {
		answer = 42,
	}`

	t.Run("disabled by default", func(t *testing.T) {
		if _, err := Parse(src); err == nil {
			t.Fatal("expecting an error when AllowReturnPrefix is disabled")
		}
	})

	t.Run("enabled via Parser", func(t *testing.T) {
		p := &Parser{AllowReturnPrefix: true}
		tbl, err := p.ParseTable(src)
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
		got, _ := tbl.Get("answer")
		if got != int64(42) {
			t.Fatalf("unexpected answer: %#v", got)
		}
	})

	t.Run("enabled via ParseModule", func(t *testing.T) {
		tbl, err := ParseModule(src)
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
		got, _ := tbl.Get("answer")
		if got != int64(42) {
			t.Fatalf("unexpected answer: %#v", got)
		}
	})

	t.Run("return is rejected inside a table", func(t *testing.T) {
		if _, err := ParseModule("{return}"); err == nil {
			t.Fatal("expecting an error for 'return' inside a table")
		}
	})
}

func TestParseNumericKeyTypes(t *testing.T) {
	tbl := MustParseTable(`{
		[1] = "one",
		[2] = "two",
		[100] = "hundred",
		[-1] = "minus one",
		[1.25] = "fraction",
		[true] = "yes",
		[false] = "no",
		["1"] = "text one",
	}`)

	wantLen := 8
	if got := tbl.Len(); got != wantLen {
		t.Fatalf("Len() = %d; want %d", got, wantLen)
	}

	cases := []struct {
		key  any
		want any
	}{
		{1, "one"},
		{2, "two"},
		{100, "hundred"},
		{-1, "minus one"},
		{1.25, "fraction"},
		{true, "yes"},
		{false, "no"},
		{"1", "text one"},
	}
	for _, tc := range cases {
		got, ok := tbl.Get(tc.key)
		if !ok {
			t.Fatalf("key %#v not found", tc.key)
		}
		if got != tc.want {
			t.Fatalf("Get(%#v) = %#v; want %#v", tc.key, got, tc.want)
		}
	}
}

func TestParseDoesNotMutateParserStateBetweenCalls(t *testing.T) {
	p := &Parser{}

	first, err := p.Parse(`{1, 2}`)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	second, err := p.Parse(`{a = 1}`)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	if !reflect.DeepEqual(first, []any{int64(1), int64(2)}) {
		t.Fatalf("unexpected first result: %#v", first)
	}
	if !reflect.DeepEqual(second, map[string]any{"a": int64(1)}) {
		t.Fatalf("unexpected second result: %#v", second)
	}
}

func TestParseBytesMatchesParse(t *testing.T) {
	src := `{a = 1, b = {2, 3}}`

	fromString, err := Parse(src)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	fromBytes, err := ParseBytes([]byte(src))
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	if !reflect.DeepEqual(fromString, fromBytes) {
		t.Fatalf("Parse and ParseBytes disagree: %#v vs %#v", fromString, fromBytes)
	}
}

func TestParseTableBytesMatchesParseTable(t *testing.T) {
	src := `{a = 1}`

	fromString := MustParseTable(src)
	fromBytes, err := ParseTableBytes([]byte(src))
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	if !reflect.DeepEqual(fromString.Entries(), fromBytes.Entries()) {
		t.Fatalf("ParseTable and ParseTableBytes disagree")
	}
}

func TestParseDeeplyNestedTables(t *testing.T) {
	const depth = 100

	src := ""
	for range depth {
		src += "{"
	}
	src += "1"
	for range depth {
		src += "}"
	}

	got, err := Parse(src)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	// Walk down the nested arrays.
	current := got
	for i := range depth - 1 {
		arr, ok := current.([]any)
		if !ok {
			t.Fatalf("level %d is %T; want []any", i, current)
		}
		current = arr[0]
	}
	if !reflect.DeepEqual(current, []any{int64(1)}) {
		t.Fatalf("unexpected innermost value: %#v", current)
	}
}

func TestParseRespectsMaxDepth(t *testing.T) {
	p := &Parser{MaxDepth: 3}

	if _, err := p.Parse("{{{1}}}"); err != nil {
		t.Fatalf("unexpected error at the depth limit: %s", err)
	}

	_, err := p.Parse("{{{{1}}}}")
	if err == nil {
		t.Fatal("expecting a depth error")
	}

	se, ok := err.(*SyntaxError)
	if !ok {
		t.Fatalf("unexpected error type %T", err)
	}
	if se.Offset != 3 {
		t.Fatalf("unexpected error offset; got %d; want 3", se.Offset)
	}
}

func TestMustParsePanicsOnError(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("MustParse must panic on invalid input")
		}
	}()

	MustParse("{a = }")
}

func TestMustParseReturnsValue(t *testing.T) {
	got := MustParse("{1, 2}")
	if !reflect.DeepEqual(got, []any{int64(1), int64(2)}) {
		t.Fatalf("unexpected value: %#v", got)
	}
}

func TestParseArrayContinuationAfterHashField(t *testing.T) {
	// Positional fields continue the array sequence regardless of interleaved
	// hash fields, exactly like Lua.
	got := parseGeneric(t, `{a = "x", 10, b = "y", 20}`)
	want := map[string]any{
		"a": "x",
		"b": "y",
		"1": int64(10),
		"2": int64(20),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected value; got %#v; want %#v", got, want)
	}
}

func TestParseDeprecatedSemicolonSeparator(t *testing.T) {
	got := parseGeneric(t, "{1; 2,}")
	if !reflect.DeepEqual(got, []any{int64(1), int64(2)}) {
		t.Fatalf("unexpected value: %#v", got)
	}
}

// TestParseReservedWordKeys documents the keyword policy: by default the parser
// is lenient and accepts reserved words as bare keys, while Parser.StrictKeywords
// rejects them exactly like the reference implementation.
func TestParseReservedWordKeys(t *testing.T) {
	t.Run("lenient by default", func(t *testing.T) {
		got, err := Parse(`{end = 1, function = 2}`)
		if err != nil {
			t.Fatalf("the default parser accepts reserved word keys: %s", err)
		}
		want := map[string]any{"end": int64(1), "function": int64(2)}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("unexpected value: %#v", got)
		}
	})

	t.Run("bracket form is always accepted", func(t *testing.T) {
		for _, p := range []*Parser{{}, {StrictKeywords: true}} {
			got, err := p.Parse(`{["end"] = 1, ["global"] = 2}`)
			if err != nil {
				t.Fatalf("the bracket form must always be accepted: %s", err)
			}
			want := map[string]any{"end": int64(1), "global": int64(2)}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("unexpected value: %#v", got)
			}
		}
	})

	t.Run("strict rejects every reserved word", func(t *testing.T) {
		strict := &Parser{StrictKeywords: true}

		for word := range reservedWords {
			src := "{" + word + " = 1}"
			if _, err := strict.Parse(src); err == nil {
				t.Fatalf("strict mode must reject %q", src)
			}
		}
	})

	t.Run("strict error message", func(t *testing.T) {
		strict := &Parser{StrictKeywords: true}
		_, err := strict.Parse(`{end = 1}`)
		if err == nil {
			t.Fatal("expecting an error")
		}
		if !strings.Contains(err.Error(), "reserved word") {
			t.Fatalf("unexpected error message: %s", err)
		}
	})

	t.Run("strict accepts neighbouring identifiers", func(t *testing.T) {
		strict := &Parser{StrictKeywords: true}
		got, err := strict.Parse(`{endgame = 1, globalish = 2}`)
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
		want := map[string]any{"endgame": int64(1), "globalish": int64(2)}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("unexpected value: %#v", got)
		}
	})
}
