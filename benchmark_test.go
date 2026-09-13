package luatable

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
)

func buildBenchmarkInput(fields int) string {
	var b strings.Builder
	b.WriteString("{\n")
	for i := range fields {
		b.WriteString("\titem")
		b.WriteString(strconv.Itoa(i))
		b.WriteString(` = { id = `)
		b.WriteString(strconv.Itoa(i))
		b.WriteString(`, name = "value-`)
		b.WriteString(strconv.Itoa(i))
		b.WriteString(`", enabled = true, ratio = 0.5},` + "\n")
	}
	b.WriteString("}\n")
	return b.String()
}

var benchmarkInputs = map[string]string{
	"small":  `{ a = 1, b = "two", c = { 1, 2, 3 } }`,
	"medium": buildBenchmarkInput(50),
	"array":  "{" + strings.Repeat("1, 2, 3, ", 300) + "}",
	"deep":   strings.Repeat("{", 50) + "1" + strings.Repeat("}", 50),
}

func BenchmarkParse(b *testing.B) {
	for name, src := range benchmarkInputs {
		b.Run(name, func(b *testing.B) {
			var p Parser
			b.SetBytes(int64(len(src)))

			for b.Loop() {
				if _, err := p.Parse(src); err != nil {
					b.Fatalf("unexpected error: %s", err)
				}
			}
		})
	}
}

func BenchmarkParseTable(b *testing.B) {
	src := benchmarkInputs["medium"]

	var p Parser
	b.SetBytes(int64(len(src)))

	for b.Loop() {
		if _, err := p.ParseTable(src); err != nil {
			b.Fatalf("unexpected error: %s", err)
		}
	}
}

func BenchmarkGet(b *testing.B) {
	src := benchmarkInputs["medium"]
	b.SetBytes(int64(len(src)))

	for b.Loop() {
		if _, ok, err := Get(src, "item25", "name"); err != nil || !ok {
			b.Fatalf("unexpected result: %v, ok = %v", err, ok)
		}
	}
}

func BenchmarkTableGetPath(b *testing.B) {
	var p Parser
	table, err := p.ParseTable(benchmarkInputs["medium"])
	if err != nil {
		b.Fatalf("unexpected error: %s", err)
	}

	b.SetBytes(int64(len(benchmarkInputs["medium"])))

	for b.Loop() {
		if _, ok := table.GetPath("item25", "name"); !ok {
			b.Fatal("the path was not found")
		}
	}
}

func BenchmarkParseBytes(b *testing.B) {
	src := []byte(benchmarkInputs["small"])

	var p Parser
	b.SetBytes(int64(len(src)))

	for b.Loop() {
		if _, err := p.ParseBytes(src); err != nil {
			b.Fatalf("unexpected error: %s", err)
		}
	}
}

func BenchmarkMarshal(b *testing.B) {
	for name, src := range benchmarkInputs {
		b.Run(name, func(b *testing.B) {
			v := MustParse(src)
			b.SetBytes(int64(len(src)))

			for b.Loop() {
				if _, err := Marshal(v); err != nil {
					b.Fatalf("unexpected error: %s", err)
				}
			}
		})
	}
}

func BenchmarkMarshalIndent(b *testing.B) {
	src := benchmarkInputs["medium"]
	v := MustParse(src)

	b.SetBytes(int64(len(src)))

	for b.Loop() {
		if _, err := MarshalIndent(v, "  "); err != nil {
			b.Fatalf("unexpected error: %s", err)
		}
	}
}

func BenchmarkMarshalTable(b *testing.B) {
	src := benchmarkInputs["medium"]

	var p Parser
	tbl, err := p.ParseTable(src)
	if err != nil {
		b.Fatalf("unexpected error: %s", err)
	}

	b.SetBytes(int64(len(src)))

	for b.Loop() {
		if _, err := Marshal(tbl); err != nil {
			b.Fatalf("unexpected error: %s", err)
		}
	}
}

// BenchmarkMarshalString measures the string writer on the three shapes it has
// to handle: a long run without escapes, escapes spread through the text, and
// non-ASCII text, which is copied in whole runs; only runs shorter than eight
// bytes are written byte by byte.
func BenchmarkMarshalString(b *testing.B) {
	cases := []struct {
		name string
		s    string
	}{
		{"plain", strings.Repeat("abcdefgh", 128)},
		{"escaped", strings.Repeat("a\"b\\c\nd\t", 128)},
		{"unicode", strings.Repeat("中文é🤭", 128)},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			b.SetBytes(int64(len(tc.s)))

			for b.Loop() {
				if _, err := Marshal([]any{tc.s}); err != nil {
					b.Fatalf("unexpected error: %s", err)
				}
			}
		})
	}
}

// BenchmarkAppendMarshal measures the buffer-reuse variant against Marshal on
// the same value: with a destination that is already large enough and an
// encoder that is reused, neither the buffer nor the key lists are allocated
// again, so repeated encoding allocates nothing at all.
func BenchmarkAppendMarshal(b *testing.B) {
	value := jsonComparisonValue(jsonComparisonFields)
	enc := new(Encoder)
	buf := make([]byte, 0, 8192)

	b.Run("reused buffer", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			var err error
			buf, err = enc.AppendMarshal(buf[:0], value)
			if err != nil {
				b.Fatalf("unexpected error: %s", err)
			}
		}
	})

	b.Run("Marshal", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			if _, err := enc.Marshal(value); err != nil {
				b.Fatalf("unexpected error: %s", err)
			}
		}
	})
}

// The benchmarks below place this package next to encoding/json on one logical
// value. They are a development reference, not a scoreboard: the formats
// differ (Lua table syntax is more verbose, a key may be a number or a
// boolean, an integer keeps its type, and comments and trailing separators are
// allowed), so the numbers answer "what does this package cost next to the
// obvious alternative", not "which format is faster".
//
// Both sides start from the same map[string]any and then parse the text back,
// so the payload is identical by construction. One real asymmetry remains:
// this package returns int64 for an integer literal while encoding/json
// returns float64 for every number, so json does less work per number here.
//
// encoding/json/v2 belongs in this comparison too, but its API requires the
// module to declare go 1.27; add it once the go directive allows that.
const jsonComparisonFields = 50

// jsonComparisonValue builds the shared payload: n records with an integer, a
// string, a boolean and a float field. It stays inside the intersection of the
// two value models.
func jsonComparisonValue(n int) map[string]any {
	value := make(map[string]any, n)
	for i := range n {
		value["item"+strconv.Itoa(i)] = map[string]any{
			"id":      int64(i),
			"name":    "value-" + strconv.Itoa(i),
			"enabled": i%2 == 0,
			"ratio":   0.5,
		}
	}
	return value
}

func BenchmarkJSONCompareParse(b *testing.B) {
	value := jsonComparisonValue(jsonComparisonFields)

	luaBytes, err := Marshal(value)
	if err != nil {
		b.Fatalf("Marshal failed: %s", err)
	}
	luaText := string(luaBytes)

	jsonSrc, err := json.Marshal(value)
	if err != nil {
		b.Fatalf("json.Marshal failed: %s", err)
	}

	b.Run("luatable", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(luaText)))

		var p Parser
		for b.Loop() {
			if _, err := p.Parse(luaText); err != nil {
				b.Fatalf("unexpected error: %s", err)
			}
		}
	})

	b.Run("encoding/json", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(jsonSrc)))

		for b.Loop() {
			var out any
			if err := json.Unmarshal(jsonSrc, &out); err != nil {
				b.Fatalf("unexpected error: %s", err)
			}
		}
	})
}

func BenchmarkJSONCompareMarshal(b *testing.B) {
	value := jsonComparisonValue(jsonComparisonFields)

	b.Run("luatable", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			if _, err := Marshal(value); err != nil {
				b.Fatalf("unexpected error: %s", err)
			}
		}
	})

	b.Run("encoding/json", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			if _, err := json.Marshal(value); err != nil {
				b.Fatalf("unexpected error: %s", err)
			}
		}
	})
}
