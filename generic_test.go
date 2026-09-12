package luatable

import (
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// Parse does not build a *Table, so the rich representation is its reference
// implementation: for every input that parses, Parse must return exactly what
// ParseTable(...).Interface() returns. The tests in this file pin that
// invariant, which is what makes the generic path safe to optimize.
//
// One input shape is exempt: a table that holds two different keys with the
// same string form ("[1]" and "[\"1\"]"). The generic map cannot tell those
// apart, and the two paths resolve the collapse differently; see
// TestParseGenericCollidingKeys for the details. The comparison below is
// structural so that such an input is still checked everywhere else: only the
// values of the colliding keys themselves are allowed to differ.

// stringKeyCollisions returns how many table keys map to each string form. A
// count above one marks a key whose value the generic representation cannot
// keep apart from another key's.
func stringKeyCollisions(table *Table) map[string]int {
	counts := make(map[string]int, len(table.entries))
	for _, entry := range table.entries {
		counts[keyToString(entry.Key)]++
	}
	return counts
}

// assertMatchesRich checks that generic is what the rich value converts to.
//
// Nested tables are compared level by level instead of through
// ToInterface, so that a table holding colliding keys is still compared
// everywhere else: its key set must be the set of string forms, and the values
// of the keys that do not collide must match. Only the values of the colliding
// keys are skipped, because those are the ones the two paths may resolve
// differently by design.
func assertMatchesRich(t *testing.T, src string, generic any, rich any) {
	t.Helper()

	table, ok := rich.(*Table)
	if !ok {
		// A leaf: nil, bool, int64, float64, string or Skipped. Nested tables
		// are always *Table in the rich representation, so there is no other
		// container to descend into.
		if !reflect.DeepEqual(generic, rich) {
			t.Fatalf("%s: got %#v; want %#v", src, generic, rich)
		}
		return
	}

	collisions := stringKeyCollisions(table)

	if table.IsArray() {
		array, ok := generic.([]any)
		if !ok {
			t.Fatalf("%s: got %T for an array table; want []any", src, generic)
		}
		if len(array) != len(table.entries) {
			t.Fatalf("%s: array has %d elements; want %d", src, len(array), len(table.entries))
		}
		for _, entry := range table.entries {
			index := entry.Key.(int64)
			assertMatchesRich(t, src, array[index-1], entry.Value)
		}
		return
	}

	genericMap, ok := generic.(map[string]any)
	if !ok {
		t.Fatalf("%s: got %T for a table that is not an array; want map[string]any", src, generic)
	}

	// The key set of the generic map is the set of string forms of the table
	// keys, whether or not any of them collide.
	forms := make(map[string]struct{}, len(table.entries))
	for _, entry := range table.entries {
		forms[keyToString(entry.Key)] = struct{}{}
	}
	if len(genericMap) != len(forms) {
		t.Fatalf("%s: got %d keys; want %d", src, len(genericMap), len(forms))
	}
	for key := range forms {
		if _, ok := genericMap[key]; !ok {
			t.Fatalf("%s: key %q is missing", src, key)
		}
	}

	for _, entry := range table.entries {
		key := keyToString(entry.Key)
		if collisions[key] > 1 {
			// The two paths may have kept a different one of the colliding
			// keys' values.
			continue
		}
		assertMatchesRich(t, src, genericMap[key], entry.Value)
	}
}

// differentialInputs is the corpus of the differential tests: the benchmark
// inputs, the fuzz seeds (which are already a curated set of edge cases) and
// inputs aimed at the array decision.
func differentialInputs() map[string]string {
	inputs := make(map[string]string, len(benchmarkInputs)+len(fuzzSeeds)+16)
	for name, src := range benchmarkInputs {
		inputs["benchmark/"+name] = src
	}
	for i, src := range fuzzSeeds {
		inputs["fuzzSeed/"+strconv.Itoa(i)] = src
	}
	for name, src := range map[string]string{
		"explicit indices out of order":  `{[2] = "b", [1] = "a"}`,
		"explicit indices with a hole":   `{[1] = "a", [3] = "c"}`,
		"positional after a key":         `{1, 2, a = 3}`,
		"positional overwritten":         `{[1] = 1, 2}`,
		"positional overwriting a key":   `{1, [1] = 2}`,
		"key overwritten twice":          `{[1] = 1, [1] = 2}`,
		"zero and negative keys":         `{[0] = "z", [-1] = "n"}`,
		"string key spelled as a number": `{["1"] = "x", ["2"] = "y"}`,
		"mixed spellings of one key":     `{[1] = "a", ["1"] = "b"}`,
		"boolean and float keys":         `{[true] = 1, [1.5] = 2}`,
		"nil positional values":          `{nil, nil}`,
		"nested map in an array":         `{1, {a = 1}, 3}`,
		"array in a map":                 `{a = {1, 2}, b = {}}`,
		"empty tables":                   `{{}, {1}, {a = 1}}`,
		"long positional run":            "{" + strings.Repeat("1, ", 300) + "}",
		"deeply nested arrays":           strings.Repeat("{", 40) + "1" + strings.Repeat("}", 40),
	} {
		inputs[name] = src
	}
	return inputs
}

// TestParseMatchesParseTable runs the differential comparison over the corpus,
// in strict and in lenient mode.
func TestParseMatchesParseTable(t *testing.T) {
	inputs := differentialInputs()

	fixtures, err := filepath.Glob(filepath.Join("testdata", "*.lua"))
	if err != nil {
		t.Fatalf("cannot list the fixtures: %s", err)
	}
	for _, path := range fixtures {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("cannot read %s: %s", path, err)
		}
		inputs["testdata/"+filepath.Base(path)] = string(data)
	}

	for _, lenient := range []bool{false, true} {
		for name, src := range inputs {
			name := name
			src := src

			t.Run(name, func(t *testing.T) {
				var p Parser
				p.Lenient = lenient
				p.AllowReturnPrefix = true

				want, errTable := p.ParseTable(src)
				got, errGeneric := p.Parse(src)

				if errTable != nil || errGeneric != nil {
					if errTable == nil || errGeneric == nil {
						t.Fatalf("one path failed and the other did not: ParseTable %v; Parse %v", errTable, errGeneric)
					}
					if errTable.Error() != errGeneric.Error() {
						t.Fatalf("the paths report different errors: ParseTable %q; Parse %q", errTable, errGeneric)
					}
					return
				}

				assertMatchesRich(t, src, got, want)
			})
		}
	}
}

// TestParseGenericArrayVerdict pins the table-wide array decision of the
// generic path: a table is an array exactly when its keys are 1..n, whichever
// way they were written.
func TestParseGenericArrayVerdict(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want any
	}{
		{"empty table is a map", `{}`, map[string]any{}},
		{"positional fields", `{1, 2}`, []any{int64(1), int64(2)}},
		{"nil values keep their slots", `{nil, nil}`, []any{nil, nil}},
		{"explicit indices", `{[1] = "a", [2] = "b"}`, []any{"a", "b"}},
		{"explicit indices out of order", `{[2] = "b", [1] = "a"}`, []any{"a", "b"}},
		{"positional field follows a key", `{[2] = "b", 1}`, []any{int64(1), "b"}},
		{"positional field overwrites a key", `{[1] = 1, 2}`, []any{int64(2)}},
		{"key overwrites a positional field", `{1, [1] = 2}`, []any{int64(2)}},
		{"hole", `{[1] = "a", [3] = "c"}`, map[string]any{"1": "a", "3": "c"}},
		{"zero key", `{[0] = "z"}`, map[string]any{"0": "z"}},
		{"negative key", `{[-1] = "n"}`, map[string]any{"-1": "n"}},
		{"string key spelled as a number", `{["1"] = "s"}`, map[string]any{"1": "s"}},
		{"boolean key", `{[true] = 1}`, map[string]any{"true": int64(1)}},
		{"float key", `{[1.5] = 1}`, map[string]any{"1.5": int64(1)}},
		{"array part of a mixed table", `{1, 2, a = 3}`,
			map[string]any{"1": int64(1), "2": int64(2), "a": int64(3)}},
		{"key first, array part after", `{a = 1, 2}`,
			map[string]any{"a": int64(1), "1": int64(2)}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Parse(tc.src)
			if err != nil {
				t.Fatalf("Parse failed: %s", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Parse(%s) = %#v; want %#v", tc.src, got, tc.want)
			}
		})
	}
}

// TestParseGenericCollidingKeys documents the one input shape where Parse and
// Table.Interface disagree. Two different Lua keys can share one string form,
// and the generic map representation can only keep one of them.
func TestParseGenericCollidingKeys(t *testing.T) {
	const src = `{[1] = "a", ["1"] = "b", [1] = "c"}`

	// The generic path writes the fields in source order, so the last
	// assignment wins, as it does in Lua.
	got, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse failed: %s", err)
	}
	if want := map[string]any{"1": "c"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Parse(%s) = %#v; want %#v", src, got, want)
	}

	// The rich path stores [1] once, at its first position, and Table.Map
	// walks the entries in that order: the value of [1] is written first and
	// then overwritten by the value of ["1"], which sits after it.
	table, err := ParseTable(src)
	if err != nil {
		t.Fatalf("ParseTable failed: %s", err)
	}
	if want := map[string]any{"1": "b"}; !reflect.DeepEqual(table.Interface(), want) {
		t.Fatalf("ParseTable(%s).Interface() = %#v; want %#v", src, table.Interface(), want)
	}
}

// TestParseMatchesParseTableOnErrorPaths checks that both paths report the same
// error, message and position included, for inputs that do not parse.
func TestParseMatchesParseTableOnErrorPaths(t *testing.T) {
	for _, src := range []string{
		"", "{", "}", "{[}", "{a =}", "{1 2}", "{'x}", "{[[x}", "{1 + 2}",
		"{[{}] = 1}", "{[nil] = 1}", "{[0x1p1024] = 1}", "return", "0x",
		strings.Repeat("{", DefaultMaxDepth+10),
		"{" + strings.Repeat("-", DefaultMaxDepth+10) + "1}",
	} {
		var p Parser
		p.AllowReturnPrefix = true

		_, errTable := p.ParseTable(src)
		_, errGeneric := p.Parse(src)
		if errTable == nil || errGeneric == nil {
			t.Fatalf("%q: one path accepted the input: ParseTable %v; Parse %v", src, errTable, errGeneric)
		}
		if errTable.Error() != errGeneric.Error() {
			t.Fatalf("%q: ParseTable reports %q; Parse reports %q", src, errTable, errGeneric)
		}
	}
}
