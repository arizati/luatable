package luatable

import (
	"strconv"
	"strings"
	"testing"
)

func buildBenchmarkInput(fields int) string {
	var b strings.Builder
	b.WriteString("{\n")
	for i := 0; i < fields; i++ {
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
