package luatable_test

import (
	"fmt"

	luatable "github.com/arizati/luatable"
)

func ExampleMarshal() {
	out, err := luatable.Marshal(map[string]any{
		"name": "demo",
		"tags": []any{"a", "b"},
	})
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println(string(out))

	// Output:
	// {name = "demo",tags = {"a","b"}}
}

func ExampleMarshalIndent() {
	out, err := luatable.MarshalIndent(map[string]any{
		"answer": int64(42),
		"nested": map[string]any{"deep": true},
	}, "  ")
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println(string(out))

	// Output:
	// {
	//   answer = 42,
	//   nested = {
	//     deep = true
	//   }
	// }
}

func ExampleMarshalModule() {
	out, err := luatable.MarshalModule(map[string]any{"debug": true})
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Print(string(out))

	// Output:
	// return {debug = true}
}

func ExampleMarshal_roundTrip() {
	out, err := luatable.Marshal([]any{int64(1), "two", 3.0})
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println(string(out))

	value, err := luatable.Parse(string(out))
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println(value)

	// Output:
	// {1,"two",3.0}
	// [1 two 3]
}
