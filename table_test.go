package luatable

import (
	"math"
	"reflect"
	"strings"
	"testing"
)

func TestTableIsArray(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want bool
	}{
		{"empty", "{}", false},
		{"single element", "{1}", true},
		{"dense array", "{1, 2, 3}", true},
		{"multi type array", `{1, "a", true, nil}`, true},
		{"explicit integer keys", "{[1]='a', [2]='b'}", true},
		{"hash table", "{a = 1}", false},
		{"mixed table", "{1, x = 2}", false},
		{"sparse integer keys", "{[1]='a', [3]='c'}", false},
		{"one based but not starting at one", "{[2]='b', [3]='c'}", false},
		{"zero key", "{[0]='z'}", false},
		{"negative key", "{[-1]='z'}", false},
		{"boolean key", "{[true]=1}", false},
		{"string key", `{["1"]="a"}`, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tbl, err := ParseTable(tc.src)
			if err != nil {
				t.Fatalf("unexpected error: %s", err)
			}
			if got := tbl.IsArray(); got != tc.want {
				t.Fatalf("IsArray() = %v; want %v", got, tc.want)
			}
		})
	}
}

func TestTableArray(t *testing.T) {
	tbl := MustParseTable(`{1, "two", true, nil}`)
	got := tbl.Array()
	want := []any{int64(1), "two", true, nil}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected Array(); got %#v; want %#v", got, want)
	}

	if got := MustParseTable(`{a = 1}`).Array(); got != nil {
		t.Fatalf("Array() on a hash table should be nil; got %#v", got)
	}
}

func TestTableMap(t *testing.T) {
	tbl := MustParseTable(`{1, 2, x = "y"}`)
	got := tbl.Map()
	want := map[string]any{
		"1": int64(1),
		"2": int64(2),
		"x": "y",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected Map(); got %#v; want %#v", got, want)
	}
}

func TestTableInterface(t *testing.T) {
	if got := MustParseTable(`{1, 2, 3}`).Interface(); !reflect.DeepEqual(got, []any{int64(1), int64(2), int64(3)}) {
		t.Fatalf("unexpected Interface() for an array table: %#v", got)
	}

	if got := MustParseTable(`{a = 1}`).Interface(); !reflect.DeepEqual(got, map[string]any{"a": int64(1)}) {
		t.Fatalf("unexpected Interface() for a hash table: %#v", got)
	}

	if got := MustParseTable(`{}`).Interface(); !reflect.DeepEqual(got, map[string]any{}) {
		t.Fatalf("unexpected Interface() for an empty table: %#v", got)
	}
}

func TestTableGet(t *testing.T) {
	tbl := MustParseTable(`{10, 20, name = "demo", [true] = false, [1.5] = "half"}`)

	cases := []struct {
		name string
		key  any
		want any
		ok   bool
	}{
		{"array index", 1, int64(10), true},
		{"array index typed int64", int64(2), int64(20), true},
		{"array index typed int32", int32(1), int64(10), true},
		{"array index typed uint", uint(1), int64(10), true},
		{"array index typed uint64", uint64(2), int64(20), true},
		{"uint64 beyond int64", uint64(math.MaxUint64), nil, false},
		{"string key", "name", "demo", true},
		{"boolean key", true, false, true},
		{"float key", 1.5, "half", true},
		{"missing array index", 3, nil, false},
		{"missing string key", "missing", nil, false},
		{"unsupported key type", []int{1}, nil, false},
		{"nil key", nil, nil, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := tbl.Get(tc.key)
			if ok != tc.ok {
				t.Fatalf("Get(%#v) ok = %v; want %v", tc.key, ok, tc.ok)
			}
			if tc.ok && !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Get(%#v) = %#v; want %#v", tc.key, got, tc.want)
			}
		})
	}
}

func TestTableEntriesPreserveOrder(t *testing.T) {
	tbl := MustParseTable(`{c = 3, a = 1, b = 2}`)
	want := []Entry{
		{Key: "c", Value: int64(3)},
		{Key: "a", Value: int64(1)},
		{Key: "b", Value: int64(2)},
	}
	if got := tbl.Entries(); !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected entries; got %#v; want %#v", got, want)
	}
}

func TestTableEntriesReturnsACopy(t *testing.T) {
	tbl := MustParseTable(`{a = 1}`)
	entries := tbl.Entries()
	entries[0].Value = "mutated"

	got, _ := tbl.Get("a")
	if got != int64(1) {
		t.Fatalf("Entries() must return a copy; table was mutated: %#v", got)
	}
}

func TestTableLen(t *testing.T) {
	if got := MustParseTable(`{1, 2, 3}`).Len(); got != 3 {
		t.Fatalf("Len() = %d; want 3", got)
	}
	if got := MustParseTable(`{}`).Len(); got != 0 {
		t.Fatalf("Len() = %d; want 0", got)
	}

	var tbl *Table
	if got := tbl.Len(); got != 0 {
		t.Fatalf("Len() on a nil table = %d; want 0", got)
	}
}

func TestTableDuplicateKeysKeepFirstPosition(t *testing.T) {
	tbl := MustParseTable(`{a = 1, b = 2, a = 3}`)
	want := []Entry{
		{Key: "a", Value: int64(3)},
		{Key: "b", Value: int64(2)},
	}
	if got := tbl.Entries(); !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected entries; got %#v; want %#v", got, want)
	}
}

func TestTablePositionalFieldOverridesEarlierKey(t *testing.T) {
	cases := []struct {
		src  string
		want []any
	}{
		{`{1, [1] = 2}`, []any{int64(2)}},
		{`{[1] = 2, 1}`, []any{int64(1)}},
		// The positional fields receive indices 1 and 2; the explicit
		// [1] = "b" overrides the first positional field.
		{`{"a", [1] = "b", "c"}`, []any{"b", "c"}},
	}
	for _, tc := range cases {
		got, err := Parse(tc.src)
		if err != nil {
			t.Fatalf("unexpected error for %q: %s", tc.src, err)
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("unexpected value for %q; got %#v; want %#v", tc.src, got, tc.want)
		}
	}
}

func TestTableIntegerValuedFloatKeyIsNormalized(t *testing.T) {
	tbl := MustParseTable(`{[1] = "a", [1.0] = "b"}`)

	if got := tbl.Len(); got != 1 {
		t.Fatalf("Len() = %d; want 1 (integer and integral float keys must collapse)", got)
	}
	for _, key := range []any{1, int64(1), 1.0, float64(1)} {
		got, ok := tbl.Get(key)
		if !ok || got != "b" {
			t.Fatalf("Get(%#v) = (%#v, %v); want (%q, true)", key, got, ok, "b")
		}
	}
}

func TestTableNestedValuesAreRichTables(t *testing.T) {
	tbl := MustParseTable(`{a = {1, 2}, b = {c = 3}}`)

	v, _ := tbl.Get("a")
	if _, ok := v.(*Table); !ok {
		t.Fatalf("nested table should be a *Table; got %T", v)
	}

	got := tbl.Interface()
	want := map[string]any{
		"a": []any{int64(1), int64(2)},
		"b": map[string]any{"c": int64(3)},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected Interface(); got %#v; want %#v", got, want)
	}
}

func TestTableString(t *testing.T) {
	cases := []struct {
		src  string
		want string
	}{
		{`{}`, `{}`},
		{`{1, 2}`, `{[1]=1, [2]=2}`},
		{`{a = "x"}`, `{a="x"}`},
		{`{["a b"] = 1}`, `{["a b"]=1}`},
	}
	for _, tc := range cases {
		got := MustParseTable(tc.src).String()
		if got != tc.want {
			t.Fatalf("String() for %q = %q; want %q", tc.src, got, tc.want)
		}
	}
}

func TestTableStringHandlesScalarValues(t *testing.T) {
	got := MustParseTable(`{1, "x", true, nil, 1.5, {n = 1}}`).String()
	for _, want := range []string{"1", `"x"`, "true", "nil", "1.5", `{n=1}`} {
		if !strings.Contains(got, want) {
			t.Fatalf("String() = %q; want it to contain %q", got, want)
		}
	}
}

func TestToInterface(t *testing.T) {
	tbl := MustParseTable(`{1, 2}`)

	if got := ToInterface(tbl); !reflect.DeepEqual(got, []any{int64(1), int64(2)}) {
		t.Fatalf("unexpected ToInterface() for a table: %#v", got)
	}
	if got := ToInterface(int64(5)); got != int64(5) {
		t.Fatalf("unexpected ToInterface() for a scalar: %#v", got)
	}
	if got := ToInterface(nil); got != nil {
		t.Fatalf("unexpected ToInterface() for nil: %#v", got)
	}
}

func TestKeyToString(t *testing.T) {
	cases := []struct {
		key  any
		want string
	}{
		{"a", "a"},
		{int64(42), "42"},
		{int64(-7), "-7"},
		{1.5, "1.5"},
		{true, "true"},
		{false, "false"},
	}
	for _, tc := range cases {
		if got := keyToString(tc.key); got != tc.want {
			t.Fatalf("keyToString(%#v) = %q; want %q", tc.key, got, tc.want)
		}
	}
}

func TestNormalizeKey(t *testing.T) {
	cases := []struct {
		key    any
		want   any
		wantOK bool
	}{
		{"s", "s", true},
		{true, true, true},
		{int(3), int64(3), true},
		{int8(3), int64(3), true},
		{int16(3), int64(3), true},
		{int32(3), int64(3), true},
		{int64(3), int64(3), true},
		{uint(3), int64(3), true},
		{uint8(3), int64(3), true},
		{uint16(3), int64(3), true},
		{uint32(3), int64(3), true},
		{uint64(3), int64(3), true},
		{uint64(math.MaxUint64), nil, false},
		{3.0, int64(3), true},
		{3.5, 3.5, true},
		{math.Inf(1), nil, false},
		{math.NaN(), nil, false},
		{nil, nil, false},
		{[]int{1}, nil, false},
	}
	for _, tc := range cases {
		got, ok := normalizeKey(tc.key)
		if ok != tc.wantOK {
			t.Fatalf("normalizeKey(%#v) ok = %v; want %v", tc.key, ok, tc.wantOK)
		}
		if ok && !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("normalizeKey(%#v) = %#v; want %#v", tc.key, got, tc.want)
		}
	}
}

func TestIsIdentifier(t *testing.T) {
	cases := []struct {
		s    string
		want bool
	}{
		{"a", true},
		{"_a1", true},
		{"A_b9", true},
		{"globalish", true}, // contains a reserved word but is not one
		{"endgame", true},
		{"", false},
		{"1a", false},
		{"a-b", false},
		{"a b", false},
		{"nil", false},
		{"true", false},
		{"return", false},
		{"end", false},
		{"global", false},
	}
	for _, tc := range cases {
		if got := isIdentifier(tc.s); got != tc.want {
			t.Fatalf("isIdentifier(%q) = %v; want %v", tc.s, got, tc.want)
		}
	}
}

// TestIsIdentifierRejectsEveryReservedWord guards the encoder: a key that is a
// reserved word must be written in bracket form, because no Lua version
// accepts "end = 1". The expected count pins the 5.1-5.5 union of reserved
// words: the 22 of Lua 5.4 plus "global", added by Lua 5.5.
func TestIsIdentifierRejectsEveryReservedWord(t *testing.T) {
	if len(reservedWords) != 23 {
		t.Fatalf("unexpected number of reserved words: %d; want 23 (Lua 5.1-5.5)", len(reservedWords))
	}
	for word := range reservedWords {
		if isIdentifier(word) {
			t.Fatalf("isIdentifier(%q) = true; reserved words must be quoted", word)
		}
	}
}
