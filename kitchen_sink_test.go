package luatable

import (
	"math"
	"reflect"
	"testing"
)

// kitchenSinkWant is the expected generic representation of
// testdata/kitchen_sink.lua. Every field of the fixture appears here, so the
// map is both the expectation and the checklist of supported constructs.
//
// The table holds both "[7] = ..." and "["7"] = ...". In the generic map
// representation a numeric key and a text key with the same spelling collide,
// so the text key wins: it is written later and Map assigns in insertion
// order. TestParseTableTestdataKitchenSink checks the distinction that the
// rich representation preserves.
func kitchenSinkWant() map[string]any {
	return map[string]any{
		// Numbers.
		"int":                  int64(42),
		"int_negative":         int64(-7),
		"int_max":              int64(math.MaxInt64),
		"float":                1.5,
		"float_leading_dot":    0.5,
		"float_trailing_dot":   1.0,
		"float_exponent":       1000.0,
		"float_small_exponent": 0.025,
		"hex":                  int64(255),
		"hex_wrapped":          int64(-1),
		"hex_float":            3.0,

		// Strings.
		"double_quoted":           "double",
		"single_quoted":           "single",
		"escaped_tab":             "a\tb",
		"escaped_newline":         "a\nb",
		"escaped_carriage_return": "a\rb",
		"escaped_controls":        "\a\b\f\v",
		"escaped_backslash":       `\`,
		"escaped_quotes":          `"'`,
		"decimal_escapes":         "ABC",
		"hex_escapes":             "AB",
		"unicode_escapes":         "HI",
		"skipped_whitespace":      "ab",
		"long_string":             "first\nsecond",
		"leveled_long_string":     "contains ]=] inside",

		// Keys.
		"spaced key": "quoted",
		"7":          "text seven",
		"-3":         "negative integer key",
		"1.5":        "float key",
		"true":       "boolean key",
		"false":      "other boolean key",
		"end":        "reserved word key",

		// Scalars.
		"nothing": nil,
		"yes":     true,
		"no":      false,

		// Structures.
		"list":               []any{int64(1), 2.5, "three", true, nil},
		"semicolons":         []any{int64(1), int64(2), int64(3)},
		"empty":              map[string]any{},
		"nested":             map[string]any{"one": map[string]any{"two": map[string]any{"three": "bottom"}}},
		"mixed":              map[string]any{"1": "first", "2": int64(2), "name": "mixed", "true": "flag"},
		"parenthesized":      int64(1),
		"unary_minus":        int64(-2),
		"trailing_separator": []any{int64(1), int64(2)},
		"last":               "done",
	}
}

// TestParseTestdataKitchenSink decodes the comprehensive fixture and checks
// every value, in both directions: no expected field may be missing and no
// decoded field may be unaccounted for.
func TestParseTestdataKitchenSink(t *testing.T) {
	value, err := Parse(readTestdata(t, "kitchen_sink.lua"))
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	root, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("the fixture should decode to a map; got %T", value)
	}

	want := kitchenSinkWant()
	for key, wantValue := range want {
		got, ok := root[key]
		if !ok {
			t.Errorf("missing key %q", key)
			continue
		}
		if !reflect.DeepEqual(got, wantValue) {
			t.Errorf("key %q: got %#v; want %#v", key, got, wantValue)
		}
	}
	for key := range root {
		if _, ok := want[key]; !ok {
			t.Errorf("unexpected key %q = %#v", key, root[key])
		}
	}
}

// TestParseTableTestdataKitchenSink checks the properties only the rich
// representation preserves: exact key types, insertion order, and the
// distinction between [7] and ["7"].
func TestParseTableTestdataKitchenSink(t *testing.T) {
	src := readTestdata(t, "kitchen_sink.lua")
	table, err := ParseTable(src)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	t.Run("exact key types", func(t *testing.T) {
		cases := []struct {
			key  any
			want any
		}{
			{"spaced key", "quoted"},
			{int64(7), "integer key"},
			{"7", "text seven"},
			{int64(-3), "negative integer key"},
			{1.5, "float key"},
			{true, "boolean key"},
			{false, "other boolean key"},
			{"end", "reserved word key"},
		}
		for _, tc := range cases {
			got, ok := table.Get(tc.key)
			if !ok {
				t.Errorf("key %#v (%T) not found", tc.key, tc.key)
				continue
			}
			if got != tc.want {
				t.Errorf("Get(%#v) = %#v; want %#v", tc.key, got, tc.want)
			}
		}
	})

	t.Run("numeric and text keys stay distinct", func(t *testing.T) {
		// Insertion order: [7] is written before ["7"].
		integer, ok := table.Get(int64(7))
		if !ok || integer != "integer key" {
			t.Fatalf("Get(7) = %#v, %v; want \"integer key\", true", integer, ok)
		}
		text, ok := table.Get("7")
		if !ok || text != "text seven" {
			t.Fatalf("Get(\"7\") = %#v, %v; want \"text seven\", true", text, ok)
		}

		// The generic projection cannot keep both; the text key wins.
		if got := table.Map()["7"]; got != "text seven" {
			t.Fatalf("Map()[\"7\"] = %#v; want \"text seven\"", got)
		}
	})

	t.Run("insertion order", func(t *testing.T) {
		entries := table.Entries()
		if len(entries) < 2 {
			t.Fatalf("unexpected entry count: %d", len(entries))
		}
		if entries[0].Key != "int" {
			t.Errorf("first entry is %#v; want the key \"int\"", entries[0].Key)
		}
		if last := entries[len(entries)-1]; last.Key != "last" {
			t.Errorf("last entry is %#v; want the key \"last\"", last.Key)
		}
	})

	t.Run("reserved word keys need strict handling", func(t *testing.T) {
		// The fixture has a bare "end = ..." key, which the lenient default
		// accepts; strict mode rejects the same text.
		strict := &Parser{StrictKeywords: true}
		if _, err := strict.ParseTable(src); err == nil {
			t.Fatal("strict mode must reject the bare reserved-word key")
		}
		// The bracket form is accepted by both modes.
		for _, p := range []*Parser{{}, strict} {
			if _, err := p.ParseTable(`{["end"] = 1}`); err != nil {
				t.Fatalf("the bracket form must be accepted: %s", err)
			}
		}
	})
}

// TestKitchenSinkRoundTrip feeds the fixture's rich representation through the
// encoder and back: the generic value must survive Marshal/Parse, the module
// form must survive MarshalModule/ParseModule, and the lenient parser must
// agree with the strict one on input that contains nothing but literals.
func TestKitchenSinkRoundTrip(t *testing.T) {
	src := readTestdata(t, "kitchen_sink.lua")

	generic, err := Parse(src)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	table, err := ParseTable(src)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	t.Run("Marshal and Parse", func(t *testing.T) {
		out, err := Marshal(table)
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
		back, err := Parse(string(out))
		if err != nil {
			t.Fatalf("the encoded fixture is not parseable: %s\n%s", err, out)
		}
		if !reflect.DeepEqual(back, generic) {
			t.Fatalf("round trip changed the value:\n got %#v\nwant %#v", back, generic)
		}
	})

	t.Run("MarshalModule and ParseModule", func(t *testing.T) {
		out, err := MarshalModule(table)
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
		back, err := ParseModule(string(out))
		if err != nil {
			t.Fatalf("the encoded module is not parseable: %s\n%s", err, out)
		}
		if !reflect.DeepEqual(back.Interface(), generic) {
			t.Fatalf("module round trip changed the value:\n got %#v\nwant %#v", back.Interface(), generic)
		}
	})

	t.Run("lenient agrees with strict", func(t *testing.T) {
		var p Parser
		p.Lenient = true
		got, err := p.Parse(src)
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
		if !reflect.DeepEqual(got, generic) {
			t.Fatalf("lenient result differs from the strict result:\n got %#v\nwant %#v", got, generic)
		}
	})

	t.Run("output is deterministic", func(t *testing.T) {
		first, err := Marshal(table)
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
		second, err := Marshal(table)
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
		if string(first) != string(second) {
			t.Fatalf("Marshal is not deterministic:\n%s\n%s", first, second)
		}
	})
}

// TestKitchenSinkSelection exercises the path lookups against the fixture, so
// the query API is covered on the same comprehensive data.
func TestKitchenSinkSelection(t *testing.T) {
	src := readTestdata(t, "kitchen_sink.lua")

	t.Run("Get", func(t *testing.T) {
		value, ok, err := Get(src, "nested", "one", "two", "three")
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
		if !ok || value != "bottom" {
			t.Fatalf("value = %#v, ok = %v; want \"bottom\", true", value, ok)
		}

		value, ok, err = Get(src, "list", 5)
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
		if !ok || value != nil {
			t.Fatalf("list[5] = %#v, ok = %v; want nil, true", value, ok)
		}
	})

	t.Run("GetAs", func(t *testing.T) {
		if got, ok, _ := GetAs[int64](src, "int_max"); !ok || got != math.MaxInt64 {
			t.Fatalf("int_max = %d, ok = %v; want %d, true", got, ok, int64(math.MaxInt64))
		}
		if got, ok, _ := GetAs[int64](src, "float_trailing_dot"); !ok || got != 1 {
			t.Fatalf("float_trailing_dot = %d, ok = %v; want 1, true", got, ok)
		}
		if got, ok, _ := GetAs[float64](src, "float_small_exponent"); !ok || got != 0.025 {
			t.Fatalf("float_small_exponent = %v, ok = %v; want 0.025, true", got, ok)
		}
		if got, ok, _ := GetAs[string](src, "spaced key"); !ok || got != "quoted" {
			t.Fatalf("spaced key = %q, ok = %v; want \"quoted\", true", got, ok)
		}
		if got, ok, _ := GetAs[string](src, "7"); !ok || got != "text seven" {
			t.Fatalf("[\"7\"] = %q, ok = %v; want \"text seven\", true", got, ok)
		}
		if got, ok, _ := GetAs[bool](src, "yes"); !ok || !got {
			t.Fatalf("yes = %v, ok = %v; want true, true", got, ok)
		}
	})

	t.Run("GetSlice", func(t *testing.T) {
		if got, ok, _ := GetSlice[int64](src, "semicolons"); !ok || !reflect.DeepEqual(got, []int64{1, 2, 3}) {
			t.Fatalf("semicolons = %v, ok = %v; want [1 2 3], true", got, ok)
		}
		if got, ok, _ := GetSlice[bool](src, "list"); ok || got != nil {
			t.Fatalf("list = %v, ok = %v; want nil, false (mixed element types)", got, ok)
		}
		if got, ok, _ := GetSlice[bool](src, "yes"); ok || got != nil {
			t.Fatalf("yes = %v, ok = %v; want nil, false (not an array)", got, ok)
		}
	})
}

// TestMarshalFloatExtremesRoundTrip pins the exact round trip of boundary
// float64 values, including the subnormal range, negative zero and the largest
// finite value. Every one of them must come back bit for bit.
func TestMarshalFloatExtremesRoundTrip(t *testing.T) {
	values := []float64{
		0,
		math.Copysign(0, -1),
		math.SmallestNonzeroFloat64,
		math.Nextafter(0, 1),
		1e-300,
		0.1,
		math.Pi,
		math.MaxFloat64,
		-math.MaxFloat64,
		math.Nextafter(1, 2),
	}

	for _, want := range values {
		out, err := Marshal([]any{want})
		if err != nil {
			t.Fatalf("Marshal(%v) failed: %s", want, err)
		}

		got, err := Parse(string(out))
		if err != nil {
			t.Fatalf("Marshal(%v) produced unparseable output %s: %s", want, out, err)
		}
		arr, ok := got.([]any)
		if !ok || len(arr) != 1 {
			t.Fatalf("unexpected parse result for %s: %#v", out, got)
		}
		back, ok := arr[0].(float64)
		if !ok {
			t.Fatalf("element of %s is %T; want float64", out, arr[0])
		}
		if back != want {
			t.Fatalf("round trip of %v produced %v (%s)", want, back, out)
		}
		if math.Signbit(back) != math.Signbit(want) {
			t.Fatalf("round trip of %v changed the sign bit (%s)", want, out)
		}
	}
}

// TestParseStringEscapesEndToEnd checks the escape sequences through the full
// parser, complementing string_test.go, which decodes raw tokens directly.
func TestParseStringEscapesEndToEnd(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"double quoted", `{a = "plain"}`, "plain"},
		{"single quoted", `{a = 'plain'}`, "plain"},
		{"backslash", `{a = "a\\b"}`, `a\b`},
		{"escaped double quote", `{a = "a\"b"}`, `a"b`},
		{"escaped single quote", `{a = 'a\'b'}`, `a'b`},
		{"control escapes", `{a = "\a\b\f\n\r\t\v"}`, "\a\b\f\n\r\t\v"},
		{"decimal escape three digits", `{a = "\65\66\67"}`, "ABC"},
		{"decimal escape followed by a digit", `{a = "\0012"}`, "\x012"},
		{"hex escape", `{a = "\x41\x42"}`, "AB"},
		{"unicode escape", `{a = "\u{4e2d}\u{6587}"}`, "中文"},
		{"z escape skips spaces", "{a = \"x\\z \t y\"}", "xy"},
		{"long string", "{a = [[line1\nline2]]}", "line1\nline2"},
		{"long string drops the first newline", "{a = [[\nbody]]}", "body"},
		{"leveled long string", `{a = [==[x]=]y]==]}`, "x]=]y"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Parse(tc.src)
			if err != nil {
				t.Fatalf("unexpected error: %s", err)
			}
			value := got.(map[string]any)["a"]
			if value != tc.want {
				t.Fatalf("value = %q; want %q", value, tc.want)
			}
		})
	}
}
