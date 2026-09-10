package luatable

import (
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
