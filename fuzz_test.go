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
		case nil, bool, int64, float64, string, []interface{}, map[string]interface{}:
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
