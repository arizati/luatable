package luatable

import (
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"reflect"
	"strings"
	"testing"
)

// mustMarshal encodes v in compact mode and fails the test on error.
func mustMarshal(t *testing.T, v any) string {
	t.Helper()

	out, err := Marshal(v)
	if err != nil {
		t.Fatalf("unexpected error for %T: %s", v, err)
	}
	return string(out)
}

func TestMarshalScalars(t *testing.T) {
	cases := []struct {
		name string
		v    any
		want string
	}{
		{"nil", nil, `nil`},
		{"true", true, `true`},
		{"false", false, `false`},

		{"int64 zero", int64(0), `0`},
		{"int64 negative", int64(-7), `-7`},
		{"int64 max", int64(math.MaxInt64), `9223372036854775807`},
		// math.MinInt64 is written in decimal, see writeInt: the hexadecimal
		// form that would preserve the type is wrong on Lua 5.1/5.2/LuaJIT.
		{"int64 min", int64(math.MinInt64), `-9223372036854775808`},

		// Convenience integer types.
		{"int", int(5), `5`},
		{"int8", int8(-8), `-8`},
		{"int16", int16(16), `16`},
		{"int32", int32(32), `32`},
		{"uint8", uint8(255), `255`},
		{"uint16", uint16(65535), `65535`},
		{"uint32", uint32(4294967295), `4294967295`},
		{"uint", uint(42), `42`},
		{"uint64", uint64(math.MaxInt64), `9223372036854775807`},

		// Floats must keep their type across a round trip, so integral values
		// get an explicit ".0".
		{"float64 one", float64(1), `1.0`},
		{"float64 negative zero", math.Copysign(0, -1), `-0.0`},
		{"float64 hundred", float64(100), `100.0`},
		{"float64 fraction", 1.5, `1.5`},
		{"float64 tenth", 0.1, `0.1`},
		{"float64 large exponent", 1e21, `1e+21`},
		{"float64 small exponent", 1e-7, `1e-07`},
		{"float32", float32(0.25), `0.25`},
		{"float32 integral", float32(2), `2.0`},

		{"string", "demo", `"demo"`},
		{"empty string", "", `""`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := mustMarshal(t, tc.v); got != tc.want {
				t.Fatalf("Marshal(%#v) = %s; want %s", tc.v, got, tc.want)
			}
		})
	}
}

func TestMarshalStringEscaping(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"quote", `a"b`, `"a\"b"`},
		{"backslash", `a\b`, `"a\\b"`},
		{"newline", "a\nb", `"a\nb"`},
		{"carriage return", "a\rb", `"a\rb"`},
		{"tab", "a\tb", `"a\tb"`},
		{"bell", "\a", `"\a"`},
		{"backspace", "\b", `"\b"`},
		{"form feed", "\f", `"\f"`},
		{"vertical tab", "\v", `"\v"`},

		// Control characters and DEL use three-digit decimal escapes.
		{"nul", "\x00", `"\000"`},
		{"unit separator", "\x1f", `"\031"`},
		{"del", "\x7f", `"\127"`},

		// Three digits are mandatory: a two-digit escape followed by a digit
		// would be read greedily by the parser.
		{"escape followed by digit", "\x01" + "2", `"\0012"`},
		{"high byte", "\xff", `"\255"`},

		// Valid UTF-8 is preserved verbatim.
		{"latin", "café", `"café"`},
		{"emoji", "🤭", `"🤭"`},
		{"chinese", "中文", `"中文"`},

		// Characters that are special to long strings or comments are
		// harmless inside a short string.
		{"long bracket", "]]", `"]]"`},
		{"comment", "--", `"--"`},
		{"leveled bracket", "[=[", `"[=["`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := mustMarshal(t, tc.in); got != tc.want {
				t.Fatalf("Marshal(%q) = %s; want %s", tc.in, got, tc.want)
			}
		})
	}
}

func TestMarshalStringRoundTrip(t *testing.T) {
	inputs := []string{
		"", "plain", `quote " inside`, `backslash \ inside`,
		"newline\nand\ttabs", "\x00\x01\x02", "\x7f", "\xff\xfe",
		"café 中文 🤭", "]] -- [=[ \a\b\f\v\r",
		strings.Repeat("x", 1000),
	}

	for _, in := range inputs {
		out, err := Marshal([]any{in})
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}

		got, err := Parse(string(out))
		if err != nil {
			t.Fatalf("Marshal(%q) produced unparseable output %s: %s", in, out, err)
		}
		arr, ok := got.([]any)
		if !ok || len(arr) != 1 {
			t.Fatalf("unexpected parse result for %q: %#v", in, got)
		}
		if arr[0] != in {
			t.Fatalf("round trip changed the string; got %q; want %q", arr[0], in)
		}
	}
}

func TestMarshalArrays(t *testing.T) {
	cases := []struct {
		name string
		v    any
		want string
	}{
		{"empty", []any{}, `{}`},
		{"single", []any{int64(1)}, `{1}`},
		{"mixed scalars", []any{int64(1), "a", nil, true, false}, `{1,"a",nil,true,false}`},
		{"nested", []any{[]any{int64(1), int64(2)}, []any{}}, `{{1,2},{}}`},
		{"strings", []string{"a", "b"}, `{"a","b"}`},
		{"ints", []int{1, 2}, `{1,2}`},
		{"int64s", []int64{1, 2}, `{1,2}`},
		{"floats", []float64{1, 2}, `{1.0,2.0}`},
		{"float32s", []float32{0.5}, `{0.5}`},
		{"bools", []bool{true, false}, `{true,false}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := mustMarshal(t, tc.v); got != tc.want {
				t.Fatalf("Marshal(%#v) = %s; want %s", tc.v, got, tc.want)
			}
		})
	}
}

func TestMarshalMaps(t *testing.T) {
	cases := []struct {
		name string
		v    any
		want string
	}{
		{"empty", map[string]any{}, `{}`},
		{"identifier key", map[string]any{"name": "demo"}, `{name = "demo"}`},
		{"sorted keys", map[string]any{"b": int64(2), "a": int64(1)}, `{a = 1,b = 2}`},
		{"non identifier key", map[string]any{"max-connections": int64(128)}, `{["max-connections"] = 128}`},
		{"empty key", map[string]any{"": int64(1)}, `{[""] = 1}`},
		{"reserved word key", map[string]any{"nil": int64(1)}, `{["nil"] = 1}`},
		{"true key", map[string]any{"true": int64(1)}, `{["true"] = 1}`},
		{"digit key", map[string]any{"1": "one"}, `{["1"] = "one"}`},
		{"unicode key", map[string]any{"中文": int64(1)}, `{["中文"] = 1}`},
		{"nested", map[string]any{"a": map[string]any{"b": int64(1)}}, `{a = {b = 1}}`},
		{"string map", map[string]string{"a": "x"}, `{a = "x"}`},
		{"int map", map[string]int{"a": 1}, `{a = 1}`},
		{"int64 map", map[string]int64{"a": 1}, `{a = 1}`},
		{"float64 map", map[string]float64{"a": 1}, `{a = 1.0}`},
		{"bool map", map[string]bool{"a": true}, `{a = true}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := mustMarshal(t, tc.v); got != tc.want {
				t.Fatalf("Marshal(%#v) = %s; want %s", tc.v, got, tc.want)
			}
		})
	}
}

func TestMarshalTables(t *testing.T) {
	cases := []struct {
		src  string
		want string
	}{
		{"{}", `{}`},
		{"{1, 2, 3}", `{1,2,3}`},
		{"{1, nil, 3}", `{1,nil,3}`},
		{"{b = 2, a = 1}", `{b = 2,a = 1}`}, // insertion order is preserved
		{"{1, x = 2}", `{[1] = 1,x = 2}`},   // mixed tables use explicit keys
		{"{[1] = 'a', [3] = 'c'}", `{[1] = "a",[3] = "c"}`},
		{"{[true] = 'x', [false] = 'y'}", `{[true] = "x",[false] = "y"}`},
		{"{[1.5] = 'h'}", `{[1.5] = "h"}`},
		{"{[10] = 'v'}", `{[10] = "v"}`},
		{"{a = {1, 2}}", `{a = {1,2}}`},
		{"{name = 'demo', items = {1, 2}}", `{name = "demo",items = {1,2}}`},
	}

	for _, tc := range cases {
		t.Run(tc.src, func(t *testing.T) {
			tbl, err := ParseTable(tc.src)
			if err != nil {
				t.Fatalf("unexpected parse error: %s", err)
			}
			if got := mustMarshal(t, tbl); got != tc.want {
				t.Fatalf("Marshal(%s) = %s; want %s", tc.src, got, tc.want)
			}
		})
	}
}

func TestMarshalTableValueIsAccepted(t *testing.T) {
	tbl := MustParseTable(`{1, 2}`)
	got := mustMarshal(t, *tbl)
	if got != `{1,2}` {
		t.Fatalf("Marshal(Table value) = %s; want {1,2}", got)
	}
}

// A pure array table is written positionally, so its elements follow index
// order 1..n even when the table was built in a different insertion order.
func TestMarshalTableArrayUsesIndexOrder(t *testing.T) {
	tbl := MustParseTable(`{[2] = "b", [1] = "a"}`)
	if got := mustMarshal(t, tbl); got != `{"a","b"}` {
		t.Fatalf("Marshal() = %s; want {\"a\",\"b\"}", got)
	}
}

func TestMarshalIndent(t *testing.T) {
	v := map[string]any{"items": []any{int64(1), int64(2)}}

	got, err := MarshalIndent(v, "  ")
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	want := "{\n" +
		"  items = {\n" +
		"    1,\n" +
		"    2\n" +
		"  }\n" +
		"}"

	if string(got) != want {
		t.Fatalf("unexpected output:\n%s\nwant:\n%s", got, want)
	}
}

func TestMarshalIndentWithTab(t *testing.T) {
	got, err := MarshalIndent([]any{[]any{int64(1)}}, "\t")
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	want := "{\n\t{\n\t\t1\n\t}\n}"
	if string(got) != want {
		t.Fatalf("unexpected output:\n%q\nwant:\n%q", got, want)
	}
}

func TestMarshalEmptyIndentIsCompact(t *testing.T) {
	got, err := MarshalIndent(map[string]any{"a": int64(1)}, "")
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if string(got) != `{a = 1}` {
		t.Fatalf("unexpected output: %s", got)
	}
}

func TestMarshalTrailingComma(t *testing.T) {
	e := Encoder{Indent: "  ", TrailingComma: true}

	got, err := e.Marshal(map[string]any{"items": []any{int64(1), int64(2)}})
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	want := "{\n" +
		"  items = {\n" +
		"    1,\n" +
		"    2,\n" +
		"  },\n" +
		"}"

	if string(got) != want {
		t.Fatalf("unexpected output:\n%s\nwant:\n%s", got, want)
	}
}

func TestMarshalTrailingCommaCompact(t *testing.T) {
	e := Encoder{TrailingComma: true}

	got, err := e.Marshal([]any{int64(1), int64(2)})
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if string(got) != `{1,2,}` {
		t.Fatalf("unexpected output: %s", got)
	}
}

func TestMarshalModule(t *testing.T) {
	v := map[string]any{"answer": int64(42)}

	got, err := MarshalModule(v)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if string(got) != "return {answer = 42}\n" {
		t.Fatalf("unexpected output: %q", got)
	}
}

func TestMarshalModuleIndent(t *testing.T) {
	got, err := MarshalModuleIndent(map[string]any{"answer": int64(42)}, "  ")
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	want := "return {\n  answer = 42\n}\n"
	if string(got) != want {
		t.Fatalf("unexpected output: %q; want %q", got, want)
	}
}

func TestMarshalModuleRoundTripsThroughParseModule(t *testing.T) {
	v := map[string]any{"answer": int64(42), "list": []any{int64(1), int64(2)}}

	out, err := MarshalModuleIndent(v, "  ")
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	tbl, err := ParseModule(string(out))
	if err != nil {
		t.Fatalf("generated module is not parseable: %s\n%s", err, out)
	}

	answer, _ := tbl.Get("answer")
	if answer != int64(42) {
		t.Fatalf("unexpected answer: %#v", answer)
	}
}

func TestMarshalIsDeterministic(t *testing.T) {
	v := map[string]any{
		"charlie": int64(3),
		"alpha":   int64(1),
		"bravo":   map[string]any{"z": int64(1), "a": int64(2)},
		"delta":   []any{int64(1), int64(2)},
	}

	first, err := MarshalIndent(v, "  ")
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	for range 50 {
		got, err := MarshalIndent(v, "  ")
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
		if string(got) != string(first) {
			t.Fatalf("output is not deterministic:\n%s\nvs\n%s", first, got)
		}
	}
}

func TestMarshalUnsortedKeysStillProducesValidLua(t *testing.T) {
	e := Encoder{UnsortedKeys: true}

	v := map[string]any{"a": int64(1), "b": int64(2), "c": int64(3)}

	for range 20 {
		out, err := e.Marshal(v)
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}

		got, err := Parse(string(out))
		if err != nil {
			t.Fatalf("output is not parseable: %s\n%s", err, out)
		}
		if !reflect.DeepEqual(got, v) {
			t.Fatalf("unexpected round trip: %#v", got)
		}
	}
}

// assertGenericRoundTrip checks invariant I2: encoding a value of the generic
// domain and parsing it back yields the very same value.
//
// Only table values are valid roots for Parse, so the value is encoded as the
// single element of an array; this also exercises the round trip of scalars.
func assertGenericRoundTrip(t *testing.T, v any) {
	t.Helper()

	out, err := Marshal([]any{v})
	if err != nil {
		t.Fatalf("Marshal(%#v) failed: %s", v, err)
	}

	got, err := Parse(string(out))
	if err != nil {
		t.Fatalf("Marshal(%#v) produced unparseable output %s: %s", v, out, err)
	}

	arr, ok := got.([]any)
	if !ok || len(arr) != 1 {
		t.Fatalf("unexpected parse result for %s: %#v", out, got)
	}

	// Documented exceptions. An empty slice and an empty map both encode to
	// "{}", which parses back as an empty map; and math.MinInt64 has no integer
	// literal form in Lua, so it comes back as the float64 of the same value.
	want := v
	if s, ok := v.([]any); ok && len(s) == 0 {
		want = map[string]any{}
	}
	if n, ok := v.(int64); ok && n == math.MinInt64 {
		want = float64(math.MinInt64)
	}

	if !reflect.DeepEqual(arr[0], want) {
		t.Fatalf("round trip of %#v via %s produced %#v; want %#v", v, out, arr[0], want)
	}
}

func TestMarshalRoundTripGenericValues(t *testing.T) {
	values := []any{
		nil, true, false,
		int64(0), int64(-1), int64(math.MaxInt64), int64(math.MinInt64),
		float64(0), float64(1), float64(-1), 1.5, -0.25, 1e21, 1e-7, 0.1,
		"", "plain", "with \"quotes\" and \\backslash",
		"newline\n\ttab", "\x00\x7f\xff", "café 🤭",
		[]any{},
		[]any{int64(1), int64(2), int64(3)},
		[]any{nil, true, "x", 1.5, []any{int64(1)}},
		map[string]any{},
		map[string]any{"a": int64(1)},
		map[string]any{"": int64(1), "nil": int64(2), "max-connections": int64(3)},
		map[string]any{"nested": map[string]any{"list": []any{int64(1), "two", nil}}},
	}

	for _, v := range values {
		assertGenericRoundTrip(t, v)
	}
}

func TestMarshalRoundTripRandomValues(t *testing.T) {
	rng := rand.New(rand.NewPCG(0x1234_5678, 0x9abc_def0))

	for range 300 {
		assertGenericRoundTrip(t, randomValue(rng, 0))
	}
}

func randomValue(rng *rand.Rand, depth int) any {
	if depth >= 3 {
		return randomScalar(rng)
	}

	switch rng.IntN(8) {
	case 0, 1, 2:
		n := rng.IntN(4) + 1
		a := make([]any, n)
		for i := range a {
			a[i] = randomValue(rng, depth+1)
		}
		return a
	case 3, 4:
		n := rng.IntN(4) + 1
		m := make(map[string]any, n)
		for i := range n {
			m[randomKey(rng, i)] = randomValue(rng, depth+1)
		}
		return m
	default:
		return randomScalar(rng)
	}
}

func randomKey(rng *rand.Rand, i int) string {
	switch rng.IntN(5) {
	case 0:
		return ""
	case 1:
		return "nil"
	case 2:
		return "\x00weird key"
	default:
		return fmt.Sprintf("key%d", i)
	}
}

func randomScalar(rng *rand.Rand) any {
	switch rng.IntN(8) {
	case 0:
		return nil
	case 1:
		return rng.IntN(2) == 0
	case 2:
		return int64(rng.IntN(2001) - 1000)
	case 3:
		return float64(rng.IntN(2001)-1000) / 8
	case 4:
		return float64(rng.IntN(1000))
	case 5:
		return ""
	case 6:
		return string(rune('a' + rng.IntN(26)))
	default:
		return "\x00\x01\"\\\n" + string(rune('a'+rng.IntN(26)))
	}
}

// TestMarshalRoundTripTable checks invariant I3 for tables whose insertion
// order is already canonical, which is what ParseTable produces in practice.
func TestMarshalRoundTripTable(t *testing.T) {
	sources := []string{
		"{}",
		"{1, 2, 3}",
		"{1, nil, 3}",
		"{b = 2, a = 1}",
		"{1, x = 2}",
		"{[1] = 'a', [3] = 'c'}",
		"{[true] = 'x', [false] = 'y'}",
		"{[1.5] = 'h'}",
		"{a = {1, 2}, b = [=[long]=]}",
		"{name = 'demo', items = {1, 2, 3}}",
	}

	for _, src := range sources {
		t.Run(src, func(t *testing.T) {
			original := MustParseTable(src)

			out, err := Marshal(original)
			if err != nil {
				t.Fatalf("Marshal failed: %s", err)
			}

			reparsed, err := ParseTable(string(out))
			if err != nil {
				t.Fatalf("Marshal produced unparseable output %s: %s", out, err)
			}

			if !tablesEquivalent(reparsed, original) {
				t.Fatalf("table changed through the round trip:\n got %#v\nwant %#v\noutput %s",
					reparsed.Entries(), original.Entries(), out)
			}
		})
	}
}

// tablesEquivalent reports whether two tables carry the same entries.
//
// Non-array tables must agree on insertion order, because the encoder
// preserves it. A pure array is written positionally, so its elements are
// compared by index rather than by insertion order.
func tablesEquivalent(a, b *Table) bool {
	if a.Len() != b.Len() {
		return false
	}

	if a.IsArray() || b.IsArray() {
		if !a.IsArray() || !b.IsArray() {
			return false
		}
		return reflect.DeepEqual(a.Interface(), b.Interface())
	}

	ea, eb := a.Entries(), b.Entries()
	for i := range ea {
		if !reflect.DeepEqual(ea[i].Key, eb[i].Key) {
			return false
		}
		if !valuesEquivalent(ea[i].Value, eb[i].Value) {
			return false
		}
	}
	return true
}

func valuesEquivalent(x, y any) bool {
	xt, xIsTable := x.(*Table)
	yt, yIsTable := y.(*Table)
	if xIsTable || yIsTable {
		return xIsTable && yIsTable && tablesEquivalent(xt, yt)
	}
	return reflect.DeepEqual(x, y)
}

func TestMarshalRoundTripParsedValuesNeverFail(t *testing.T) {
	sources := []string{
		"{}",
		"{1, 2, 3}",
		"{a = 1, b = 'two', c = {true, false, nil}}",
		"{[1.5] = 'x', [true] = 'y', [10] = 'z'}",
		"{[ [[]] ] = 1}",
		"{x = 0x1p4, y = 1e21, z = 0.1}",
		"{long = [[\nline1\nline2]]}",
	}

	for _, src := range sources {
		v, err := Parse(src)
		if err != nil {
			t.Fatalf("unexpected parse error for %s: %s", src, err)
		}

		out, err := Marshal(v)
		if err != nil {
			t.Fatalf("Marshal of parsed value failed for %s: %s", src, err)
		}

		got, err := Parse(string(out))
		if err != nil {
			t.Fatalf("Marshal produced unparseable output %s: %s", out, err)
		}
		if !reflect.DeepEqual(got, v) {
			t.Fatalf("round trip of %s produced %#v; want %#v", src, got, v)
		}
	}
}

func TestMarshalErrors(t *testing.T) {
	type point struct{ X, Y int }
	var nilPointer *point

	cases := []struct {
		name     string
		v        any
		wantMsg  string
		wantPath string
	}{
		{
			name:    "struct",
			v:       point{1, 2},
			wantMsg: "unsupported value type luatable.point",
		},
		{
			name:    "pointer",
			v:       &point{1, 2},
			wantMsg: "unsupported value type *luatable.point",
		},
		{
			name:    "nil pointer",
			v:       nilPointer,
			wantMsg: "unsupported value type *luatable.point",
		},
		{
			name:    "func",
			v:       func() {},
			wantMsg: "unsupported value type func()",
		},
		{
			name:    "chan",
			v:       make(chan int),
			wantMsg: "unsupported value type chan int",
		},
		{
			name:    "complex",
			v:       complex(1, 2),
			wantMsg: "unsupported value type complex128",
		},
		{
			name:     "slice element",
			v:        []any{point{}},
			wantMsg:  "unsupported value type luatable.point",
			wantPath: "[0]",
		},
		{
			name:     "map value",
			v:        map[string]any{"a": map[string]any{"b": point{}}},
			wantMsg:  "unsupported value type luatable.point",
			wantPath: ".a.b",
		},
		{
			name:     "non identifier key path",
			v:        map[string]any{"max-connections": point{}},
			wantMsg:  "unsupported value type luatable.point",
			wantPath: `["max-connections"]`,
		},
		{
			name:    "nan",
			v:       math.NaN(),
			wantMsg: "NaN cannot be represented",
		},
		{
			name:     "positive infinity in a map",
			v:        map[string]any{"inf": math.Inf(1)},
			wantMsg:  "+Inf cannot be represented",
			wantPath: ".inf",
		},
		{
			name:    "negative infinity",
			v:       math.Inf(-1),
			wantMsg: "-Inf cannot be represented",
		},
		{
			name:    "uint64 overflow",
			v:       uint64(math.MaxUint64),
			wantMsg: "unsigned value 18446744073709551615 does not fit into int64",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Marshal(tc.v)
			if err == nil {
				t.Fatalf("expecting an error for %T", tc.v)
			}

			var ee *EncodeError
			if !errors.As(err, &ee) {
				t.Fatalf("error %T is not an *EncodeError: %s", err, err)
			}
			if !strings.Contains(ee.Msg, tc.wantMsg) {
				t.Fatalf("unexpected message %q; want it to contain %q", ee.Msg, tc.wantMsg)
			}
			if ee.Path != tc.wantPath {
				t.Fatalf("unexpected path %q; want %q", ee.Path, tc.wantPath)
			}
		})
	}
}

func TestMarshalDepthLimit(t *testing.T) {
	e := Encoder{MaxDepth: 2}

	if _, err := e.Marshal([]any{[]any{int64(1)}}); err != nil {
		t.Fatalf("unexpected error within the depth limit: %s", err)
	}

	_, err := e.Marshal([]any{[]any{[]any{int64(1)}}})
	if err == nil {
		t.Fatal("expecting a depth error")
	}

	var ee *EncodeError
	if !errors.As(err, &ee) {
		t.Fatalf("unexpected error type %T", err)
	}
	if !strings.Contains(ee.Msg, "nesting depth exceeds the maximum of 2") {
		t.Fatalf("unexpected message: %s", ee.Msg)
	}
	if ee.Path != "[0][0]" {
		t.Fatalf("unexpected path %q; want [0][0]", ee.Path)
	}
}

func TestMarshalDeeplyNestedValue(t *testing.T) {
	// A value nested DefaultMaxDepth levels deep must still encode.
	v := any(int64(1))
	for range DefaultMaxDepth - 1 {
		v = []any{v}
	}

	if _, err := Marshal(v); err != nil {
		t.Fatalf("unexpected error at the depth limit: %s", err)
	}
}

func TestEncodeErrorFormat(t *testing.T) {
	cases := []struct {
		err  *EncodeError
		want string
	}{
		{
			err:  &EncodeError{Msg: "boom"},
			want: "luatable: encode error: boom",
		},
		{
			err:  &EncodeError{Msg: "boom", Path: ".a[0]"},
			want: "luatable: encode error at .a[0]: boom",
		},
	}

	for _, tc := range cases {
		if got := tc.err.Error(); got != tc.want {
			t.Fatalf("unexpected Error() string; got %q; want %q", got, tc.want)
		}
	}
}

func TestMarshalEmptyContainers(t *testing.T) {
	cases := []struct {
		name string
		v    any
	}{
		{"empty slice", []any{}},
		{"empty string slice", []string{}},
		{"empty map", map[string]any{}},
		{"empty string map", map[string]string{}},
		{"empty table", MustParseTable(`{}`)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := mustMarshal(t, tc.v); got != `{}` {
				t.Fatalf("Marshal() = %s; want {}", got)
			}
		})
	}
}

// TestMarshalQuotesReservedWordKeys checks that every reserved word of Lua 5.1
// through 5.5 is written in bracket form. A bare "end = 1" would be rejected
// by every Lua implementation, which is exactly the bug this guards against.
func TestMarshalQuotesReservedWordKeys(t *testing.T) {
	for word := range reservedWords {
		t.Run(word, func(t *testing.T) {
			got := mustMarshal(t, map[string]any{word: int64(1)})
			want := `{["` + word + `"] = 1}`
			if got != want {
				t.Fatalf("Marshal(%q key) = %s; want %s", word, got, want)
			}

			// The quoted key must still round trip.
			parsed, err := Parse(got)
			if err != nil {
				t.Fatalf("unexpected parse error for %s: %s", got, err)
			}
			if !reflect.DeepEqual(parsed, map[string]any{word: int64(1)}) {
				t.Fatalf("unexpected round trip for %s: %#v", got, parsed)
			}
		})
	}
}

// TestMarshalQuotesReservedWordsInsideTables covers the same rule for the keys
// produced by *Table and for the debug rendering of Table.String.
func TestMarshalQuotesReservedWordsInsideTables(t *testing.T) {
	tbl := MustParseTable(`{end = 1, ["function"] = 2, ok = 3}`)

	if got := mustMarshal(t, tbl); got != `{["end"] = 1,["function"] = 2,ok = 3}` {
		t.Fatalf("unexpected Marshal output: %s", got)
	}
	if got := tbl.String(); got != `{["end"]=1, ["function"]=2, ok=3}` {
		t.Fatalf("unexpected String output: %s", got)
	}
}

// TestAppendMarshal checks the append variant against Marshal, the reuse of one
// destination across calls, and the contract that a failed encode leaves the
// destination alone.
func TestAppendMarshal(t *testing.T) {
	values := []any{
		map[string]any{"name": "demo", "tags": []any{"a", "b"}},
		[]any{int64(1), 2.5, "x", nil, true},
		MustParseTable(`{list = {1, 2, 3}, ["end"] = true}`),
		"plain string",
		int64(0),
	}

	enc := new(Encoder)

	t.Run("matches Marshal", func(t *testing.T) {
		for _, v := range values {
			want, err := Marshal(v)
			if err != nil {
				t.Fatalf("Marshal(%#v) failed: %s", v, err)
			}

			got, err := enc.AppendMarshal(nil, v)
			if err != nil {
				t.Fatalf("AppendMarshal(%#v) failed: %s", v, err)
			}
			if string(got) != string(want) {
				t.Fatalf("AppendMarshal(%#v) = %s; want %s", v, got, want)
			}
		}
	})

	t.Run("reuses one destination", func(t *testing.T) {
		buf := make([]byte, 0, 256)

		for _, v := range values {
			want, err := Marshal(v)
			if err != nil {
				t.Fatalf("Marshal(%#v) failed: %s", v, err)
			}

			// Reslicing to zero length keeps the capacity, which is the
			// documented way to encode into a reused buffer.
			buf, err = enc.AppendMarshal(buf[:0], v)
			if err != nil {
				t.Fatalf("AppendMarshal(%#v) failed: %s", v, err)
			}
			if string(buf) != string(want) {
				t.Fatalf("AppendMarshal into a reused buffer = %s; want %s", buf, want)
			}
		}
	})

	t.Run("appends after existing content", func(t *testing.T) {
		buf := []byte("prefix:")
		var err error

		buf, err = enc.AppendMarshal(buf, int64(1))
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
		buf, err = enc.AppendMarshal(buf, "two")
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}

		if got, want := string(buf), `prefix:1"two"`; got != want {
			t.Fatalf("accumulated output = %q; want %q", got, want)
		}
	})

	t.Run("a failed encode leaves dst unchanged", func(t *testing.T) {
		dst := []byte("unchanged")

		got, err := enc.AppendMarshal(dst, math.NaN())
		if err == nil {
			t.Fatal("expecting an error")
		}
		var ee *EncodeError
		if !errors.As(err, &ee) {
			t.Fatalf("error %T is not an *EncodeError: %s", err, err)
		}
		if string(got) != "unchanged" {
			t.Fatalf("dst after a failed encode = %q; want %q", got, "unchanged")
		}
	})
}

// TestEncoderReusesMapKeyLists covers the per-depth key scratch of the encoder:
// the first map with more than eight keys allocates the list, a later larger
// map grows it, a smaller one reuses it, and the output stays identical to a
// fresh encoder's on every pass.
func TestEncoderReusesMapKeyLists(t *testing.T) {
	records := func(n int, tag string) map[string]any {
		m := make(map[string]any, n)
		for i := range n {
			m[fmt.Sprintf("k%02d-%s", i, tag)] = int64(i)
		}
		return m
	}

	// The three maps sit at the same depth, so they share one scratch slot:
	// 12 keys allocate it, 20 grow it, and 9 reuse it.
	value := map[string]any{
		"first":  records(12, "a"),
		"second": records(20, "b"),
		"third":  records(9, "c"),
	}

	want, err := Marshal(value)
	if err != nil {
		t.Fatalf("Marshal failed: %s", err)
	}

	enc := new(Encoder)
	for pass := range 3 {
		got, err := enc.Marshal(value)
		if err != nil {
			t.Fatalf("pass %d failed: %s", pass, err)
		}
		if string(got) != string(want) {
			t.Fatalf("pass %d differs from a fresh encoder:\n got %s\nwant %s", pass, got, want)
		}
	}
}
