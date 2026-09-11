package luatable

import (
	"math"
	"strings"
	"testing"
)

var fuzzSeeds = []string{
	"{}",
	"{1, 2, 3}",
	`{a = 1, b = "two"}`,
	`{["k"] = 1, [true] = 2, [1.5] = 3}`,
	"{a = {b = {c = 1}}}",
	"{{}, {{}}}",
	"{1; 2; 3;}",
	"{-1, -2.5, - 3}",
	`{[[long]]}`,
	"{[=[a]]b]=]}",
	"{[ [[k]] ] = 1}",
	`{a = "\n\t\u{1F600}\65\x41"}`,
	"{0xFF, 0x1p4, 1e10, .5}",
	"-- a comment\n{1, --[[ inline ]] 2}",
	"--[==[ long ]] comment ]==]\n{}",
	"",
	"{",
	"}",
	"{[}",
	"return {1}",
	"{{{",
	"{a=}",
	"{\"unterminated}",
	"{[[unterminated}",
	"0x",
	"\x00\x01\x02",
	strings.Repeat("{", DefaultMaxDepth+10),
	"{" + strings.Repeat("(", DefaultMaxDepth+10) + "1" + strings.Repeat(")", DefaultMaxDepth+10) + "}",
	"{" + strings.Repeat("-", DefaultMaxDepth+10) + "1}",

	// Reserved words: accepted as bare keys by the lenient parser, and quoted
	// by the encoder.
	"{end = 1, function = 2, global = 3, goto = 4}",
	"{nil = 1}",

	// Literals outside the float64 range evaluate to ±Inf or to zero, as every
	// reference implementation does; the encoder rejects the resulting
	// infinities.
	"{1e400}",
	"{-1e400}",
	"{0x1p1024}",
	"{0x1p-1100}",
	"{[0x1p1024] = 1}",
}

// containsInf reports whether v holds a float64 infinity anywhere inside it,
// in either the generic or the rich table representation. Parse produces such
// values from literals outside the float64 range (1e400, 0x1p1024); Marshal
// rejects them because no Lua literal denotes an infinity.
func containsInf(v any) bool {
	switch x := v.(type) {
	case float64:
		return math.IsInf(x, 0)
	case []any:
		for _, e := range x {
			if containsInf(e) {
				return true
			}
		}
	case map[string]any:
		for _, e := range x {
			if containsInf(e) {
				return true
			}
		}
	case *Table:
		for _, e := range x.entries {
			if containsInf(e.Value) {
				return true
			}
		}
	}
	return false
}

func FuzzParse(f *testing.F) {
	for _, s := range fuzzSeeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, s string) {
		v, err := Parse(s)
		if err != nil {
			return
		}
		switch v.(type) {
		case nil, bool, int64, float64, string, []any, map[string]any:
		default:
			t.Fatalf("unexpected generic value type %T", v)
		}
	})
}

// containsSkipped reports whether v holds a Skipped value anywhere inside it,
// in either the generic or the rich table representation.
func containsSkipped(v any) bool {
	switch x := v.(type) {
	case Skipped:
		return true
	case []any:
		for _, e := range x {
			if containsSkipped(e) {
				return true
			}
		}
	case map[string]any:
		for _, e := range x {
			if containsSkipped(e) {
				return true
			}
		}
	case *Table:
		for _, e := range x.entries {
			if containsSkipped(e.Value) {
				return true
			}
		}
	}
	return false
}

// FuzzParseLenient checks that the recovery mode terminates and does not panic
// whatever it is fed. Skipping works on tokens and is iterative, so input that
// no Lua implementation would accept still ends in a result or an error.
func FuzzParseLenient(f *testing.F) {
	for _, s := range fuzzSeeds {
		f.Add(s)
	}
	for _, s := range []string{
		`{f = function() return 1, 2 end}`,
		`{a = string.format, b = loadstring("\27LJ"), c = math.huge}`,
		`{a = 1 + 2, b = f(g()), c = #t, d = "x" .. "y"}`,
		`{1, f(), 3}`,
		`{[f()] = 1, ok = 2}`,
	} {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, s string) {
		var p Parser
		p.Lenient = true
		table, err := p.ParseTable(s)
		if err != nil {
			return
		}

		// None of these conversions may panic for a recovered table.
		_ = table.Interface()
		_ = table.Map()
		_ = table.Array()
		_ = table.Len()
		_ = table.Entries()
		_ = table.String()

		// A skipped value has no representation in Lua source, so the encoder
		// must reject it rather than write the raw text back.
		if containsSkipped(table) {
			if _, err := Marshal(table); err == nil {
				t.Fatal("Marshal accepted a rich table holding a Skipped value")
			}
			if _, err := Marshal(table.Interface()); err == nil {
				t.Fatal("Marshal accepted a generic value holding a Skipped value")
			}

			// Writing the text back is an explicit choice. It may fail for
			// other reasons (an infinity elsewhere, the depth limit), and its
			// output is not promised to be valid Lua, but it must not panic.
			e := Encoder{EmitSkipped: true}
			_, _ = e.Marshal(table)
			_, _ = e.Marshal(table.Interface())
		}
	})
}

func FuzzParseTable(f *testing.F) {
	for _, s := range fuzzSeeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, s string) {
		table, err := ParseTable(s)
		if err != nil {
			return
		}
		// None of these conversions may panic or hang for a parsed table.
		_ = table.Interface()
		_ = table.Map()
		_ = table.Array()
		_ = table.Len()
		_ = table.Entries()
		_ = table.String()
	})
}

func FuzzParseModule(f *testing.F) {
	f.Add("return {a = 1}")
	f.Add("return {}")
	f.Add("{a = 1}")

	f.Fuzz(func(t *testing.T, s string) {
		_, _ = ParseModule(s)
	})
}

// FuzzMarshalString checks that any Go string survives a byte-exact round trip
// through the encoder and the parser.
func FuzzMarshalString(f *testing.F) {
	for _, s := range []string{
		"", "plain", `quote " backslash \`, "line\nbreak",
		"\x00\x01\x7f\xff", "café 🤭", "]] -- [=[ \a\b\f\v\r",
	} {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, s string) {
		out, err := Marshal([]any{s})
		if err != nil {
			t.Fatalf("Marshal failed: %s", err)
		}

		got, err := Parse(string(out))
		if err != nil {
			t.Fatalf("encoded %s is not parseable: %s", out, err)
		}

		arr, ok := got.([]any)
		if !ok || len(arr) != 1 {
			t.Fatalf("unexpected parse result: %#v", got)
		}
		if arr[0] != s {
			t.Fatalf("string changed through the round trip: got %q; want %q", arr[0], s)
		}
	})
}

// FuzzMarshalParseTable checks that encoding a parsed table and parsing it back
// preserves the table, including the key types of non-array tables.
func FuzzMarshalParseTable(f *testing.F) {
	for _, s := range fuzzSeeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, s string) {
		original, err := ParseTable(s)
		if err != nil {
			return
		}
		// Infinities have no Lua literal, so Marshal rejects them; they are
		// the one documented exception to the round trip invariant.
		if containsInf(original) {
			return
		}

		out, err := Marshal(original)
		if err != nil {
			t.Fatalf("Marshal failed for %q: %s", s, err)
		}

		reparsed, err := ParseTable(string(out))
		if err != nil {
			t.Fatalf("encoded %s is not parseable: %s", out, err)
		}

		if !tablesEquivalent(reparsed, original) {
			t.Fatalf("table changed: got %#v; want %#v (source %q, output %s)",
				reparsed.Entries(), original.Entries(), s, out)
		}
	})
}

// FuzzMarshalGeneric checks that whatever Parse accepts, Marshal encodes into
// something Parse (and ParseModule) accepts again.
func FuzzMarshalGeneric(f *testing.F) {
	for _, s := range fuzzSeeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, s string) {
		v, err := Parse(s)
		if err != nil {
			return
		}
		// Infinities have no Lua literal, so Marshal rejects them; they are
		// the one documented exception to the round trip invariant.
		if containsInf(v) {
			return
		}

		compact, err := Marshal(v)
		if err != nil {
			t.Fatalf("Marshal failed for %q: %s", s, err)
		}
		if _, err := Parse(string(compact)); err != nil {
			t.Fatalf("compact output %s is not parseable: %s", compact, err)
		}

		indented, err := MarshalIndent(v, "  ")
		if err != nil {
			t.Fatalf("MarshalIndent failed for %q: %s", s, err)
		}
		if _, err := Parse(string(indented)); err != nil {
			t.Fatalf("indented output %s is not parseable: %s", indented, err)
		}

		module, err := MarshalModuleIndent(v, "\t")
		if err != nil {
			t.Fatalf("MarshalModuleIndent failed for %q: %s", s, err)
		}
		if _, err := ParseModule(string(module)); err != nil {
			t.Fatalf("module output %s is not parseable: %s", module, err)
		}
	})
}
