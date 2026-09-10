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
