package luatable_test

import (
	"errors"
	"fmt"

	luatable "github.com/arizati/luatable"
)

func ExampleParse() {
	value, err := luatable.Parse(`{ name = "demo", items = { 1, 2, 3 } }`)
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	table := value.(map[string]any)
	fmt.Println(table["name"])
	fmt.Println(table["items"])

	// Output:
	// demo
	// [1 2 3]
}

func ExampleParse_array() {
	value, err := luatable.Parse(`{ "a", "b", "c" }`)
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println(value.([]any))

	// Output:
	// [a b c]
}

func ExampleParseTable() {
	table, err := luatable.ParseTable(`{ 1, 2, kind = "demo" }`)
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	for _, entry := range table.Entries() {
		fmt.Printf("%v=%v\n", entry.Key, entry.Value)
	}

	// Output:
	// 1=1
	// 2=2
	// kind=demo
}

func ExampleParseModule() {
	table, err := luatable.ParseModule("return {\n\tdebug = true,\n}")
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	debug, _ := table.Get("debug")
	fmt.Println(debug)

	// Output:
	// true
}

func ExampleParser() {
	var p luatable.Parser

	value, err := p.Parse(`{ 1, [true] = "yes" }`)
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println(value)

	// Output:
	// map[1:1 true:yes]
}

func ExampleSyntaxError() {
	_, err := luatable.Parse("{ a = }")

	var syntaxErr *luatable.SyntaxError
	if errors.As(err, &syntaxErr) {
		fmt.Printf("line %d, column %d: %s\n", syntaxErr.Line, syntaxErr.Column, syntaxErr.Msg)
	}

	// Output:
	// line 1, column 7: unexpected '}', expected a value
}

func ExampleGetAs() {
	src := `{ name = "demo", ports = { 8080, 9090 } }`

	name, ok, err := luatable.GetAs[string](src, "name")
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println(name, ok)

	// Positional keys start at 1, as they do in Lua.
	first, ok, _ := luatable.GetAs[int64](src, "ports", 1)
	fmt.Println(first, ok)

	ports, ok, _ := luatable.GetSlice[int64](src, "ports")
	fmt.Println(ports, ok)

	// Output:
	// demo true
	// 8080 true
	// [8080 9090] true
}

func ExampleParser_lenient() {
	var p luatable.Parser
	p.Lenient = true

	table, err := p.ParseTable(`{ id = 1, fn = loadstring("\27LJ") }`)
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	id, _ := table.Get("id")
	fmt.Println(id)

	fn, _ := table.Get("fn")
	fmt.Println(fn.(luatable.Skipped).Text)

	// Output:
	// 1
	// loadstring("\27LJ")
}
