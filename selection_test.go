package luatable

import (
	"errors"
	"reflect"
	"testing"
)

const selectionSrc = `{
	name = "demo",
	items = { "first", "second" },
	servers = {
		{ port = 8080, tags = { "a" } },
		{ port = 9090 },
	},
	[true] = "flag",
	[1.5] = "float key",
	["1"] = "string key",
	nothing = nil,
}`

func TestGetPath(t *testing.T) {
	table := MustParseTable(selectionSrc)

	cases := []struct {
		name string
		path []any
		want any
	}{
		{"empty path", nil, table},
		{"string key", []any{"name"}, "demo"},
		{"first element", []any{"items", 1}, "first"},
		{"int64 key", []any{"items", int64(2)}, "second"},
		{"float key with an integral value", []any{"items", 2.0}, "second"},
		{"nested walk", []any{"servers", 1, "port"}, int64(8080)},
		{"deeper walk", []any{"servers", 1, "tags", 1}, "a"},
		{"second nested walk", []any{"servers", 2, "port"}, int64(9090)},
		{"boolean key", []any{true}, "flag"},
		{"float key", []any{1.5}, "float key"},
		{"string key that looks like a number", []any{"1"}, "string key"},
		{"field holding nil", []any{"nothing"}, nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := table.GetPath(tc.path...)
			if !ok {
				t.Fatalf("GetPath(%v) reported a missing path", tc.path)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("GetPath(%v) = %#v; want %#v", tc.path, got, tc.want)
			}
		})
	}
}

func TestGetPathMissing(t *testing.T) {
	table := MustParseTable(selectionSrc)

	paths := [][]any{
		{"nope"},
		{"items", 0},
		{"items", 3},
		{"items", -1},
		{"name", "x"},     // walking into a string
		{"items", 1, "x"}, // walking into a string element
		{"nothing", "x"},  // walking into nil
		{1, "x"},          // the table has no positional key 1
		{struct{}{}},      // not a valid table key
	}

	for _, path := range paths {
		if value, ok := table.GetPath(path...); ok {
			t.Fatalf("GetPath(%v) = %#v, true; want ok = false", path, value)
		}
	}
}

func TestGet(t *testing.T) {
	t.Run("module prefix and generic projection", func(t *testing.T) {
		value, ok, err := Get(`return { list = { 1, 2 }, name = "demo" }`, "list")
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
		if !ok {
			t.Fatal("the path was not found")
		}
		if want := []any{int64(1), int64(2)}; !reflect.DeepEqual(value, want) {
			t.Fatalf("value = %#v; want %#v", value, want)
		}
	})

	t.Run("bare constructor", func(t *testing.T) {
		value, ok, err := Get(`{ a = { b = { c = 1 } } }`, "a", "b", "c")
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
		if !ok || value != int64(1) {
			t.Fatalf("value = %#v, ok = %v; want 1, true", value, ok)
		}
	})

	t.Run("lenient by default", func(t *testing.T) {
		src := `{ id = 1, fn = loadstring("x"), huge = math.huge }`
		if _, err := Parse(src); err == nil {
			t.Fatal("the strict parser must reject this input, which is why Get recovers")
		}

		value, ok, err := Get(src, "id")
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
		if !ok || value != int64(1) {
			t.Fatalf("id = %#v, ok = %v; want 1, true", value, ok)
		}

		value, ok, err = Get(src, "fn")
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
		skipped, isSkipped := value.(Skipped)
		if !ok || !isSkipped {
			t.Fatalf("fn = %#v, ok = %v; want a Skipped value", value, ok)
		}
		if skipped.Text != `loadstring("x")` {
			t.Fatalf("fn.Text = %q; want %q", skipped.Text, `loadstring("x")`)
		}
	})

	t.Run("missing path", func(t *testing.T) {
		value, ok, err := Get(`{a = 1}`, "b")
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
		if ok || value != nil {
			t.Fatalf("value = %#v, ok = %v; want nil, false", value, ok)
		}
	})

	t.Run("field holding nil", func(t *testing.T) {
		value, ok, err := Get(`{a = nil}`, "a")
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
		if !ok || value != nil {
			t.Fatalf("value = %#v, ok = %v; want nil, true", value, ok)
		}
	})

	t.Run("structural error", func(t *testing.T) {
		_, _, err := Get(`{a = }`, "a")
		var syntaxErr *SyntaxError
		if !errors.As(err, &syntaxErr) {
			t.Fatalf("error %v is not a *SyntaxError", err)
		}
	})
}

const scalarSrc = `{
	port = 8080,
	ratio = 2.5,
	whole = 3.0,
	name = "demo",
	flag = true,
	nothing = nil,
	skipped = f(),
}`

func TestGetAs(t *testing.T) {
	t.Run("int64", func(t *testing.T) {
		port, ok, err := GetAs[int64](scalarSrc, "port")
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
		if !ok || port != 8080 {
			t.Fatalf("port = %d, ok = %v; want 8080, true", port, ok)
		}

		// A float with an integral value converts, as math.tointeger does.
		if whole, ok, _ := GetAs[int64](scalarSrc, "whole"); !ok || whole != 3 {
			t.Fatalf("whole = %d, ok = %v; want 3, true", whole, ok)
		}

		for _, path := range [][]any{{"ratio"}, {"name"}, {"flag"}, {"nothing"}, {"skipped"}, {"nope"}} {
			if value, ok, err := GetAs[int64](scalarSrc, path...); err != nil || ok || value != 0 {
				t.Fatalf("GetAs[int64](%v) = %d, %v, %v; want 0, false, nil", path, value, ok, err)
			}
		}
	})

	t.Run("float64", func(t *testing.T) {
		if ratio, ok, _ := GetAs[float64](scalarSrc, "ratio"); !ok || ratio != 2.5 {
			t.Fatalf("ratio = %v, ok = %v; want 2.5, true", ratio, ok)
		}
		if port, ok, _ := GetAs[float64](scalarSrc, "port"); !ok || port != 8080 {
			t.Fatalf("port = %v, ok = %v; want 8080, true", port, ok)
		}
		if name, ok, _ := GetAs[float64](scalarSrc, "name"); ok || name != 0 {
			t.Fatalf("name = %v, ok = %v; want 0, false", name, ok)
		}
	})

	t.Run("string and bool", func(t *testing.T) {
		if name, ok, _ := GetAs[string](scalarSrc, "name"); !ok || name != "demo" {
			t.Fatalf("name = %q, ok = %v; want \"demo\", true", name, ok)
		}
		if flag, ok, _ := GetAs[bool](scalarSrc, "flag"); !ok || !flag {
			t.Fatalf("flag = %v, ok = %v; want true, true", flag, ok)
		}
		if name, ok, _ := GetAs[string](scalarSrc, "port"); ok || name != "" {
			t.Fatalf("string of a number = %q, ok = %v; want \"\", false", name, ok)
		}
		if flag, ok, _ := GetAs[bool](scalarSrc, "nothing"); ok || flag {
			t.Fatalf("bool of nil = %v, ok = %v; want false, false", flag, ok)
		}
	})

	t.Run("module prefix", func(t *testing.T) {
		port, ok, err := GetAs[int64](`return { port = 8080 }`, "port")
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
		if !ok || port != 8080 {
			t.Fatalf("port = %d, ok = %v; want 8080, true", port, ok)
		}
	})

	t.Run("structural error", func(t *testing.T) {
		_, _, err := GetAs[int64](`{a = }`, "a")
		var syntaxErr *SyntaxError
		if !errors.As(err, &syntaxErr) {
			t.Fatalf("error %v is not a *SyntaxError", err)
		}
	})
}

func TestGetSlice(t *testing.T) {
	src := `{
		ports = { 8080, 9090 },
		names = { "a", "b" },
		mixed = { 1, "x" },
		empty = {},
		keyed = { a = 1 },
		nothing = nil,
	}`

	t.Run("int64", func(t *testing.T) {
		ports, ok, err := GetSlice[int64](src, "ports")
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
		if !ok || !reflect.DeepEqual(ports, []int64{8080, 9090}) {
			t.Fatalf("ports = %v, ok = %v; want [8080 9090], true", ports, ok)
		}
	})

	t.Run("float64 from integers", func(t *testing.T) {
		ports, ok, _ := GetSlice[float64](src, "ports")
		if !ok || !reflect.DeepEqual(ports, []float64{8080, 9090}) {
			t.Fatalf("ports = %v, ok = %v; want [8080 9090], true", ports, ok)
		}
	})

	t.Run("strings", func(t *testing.T) {
		names, ok, _ := GetSlice[string](src, "names")
		if !ok || !reflect.DeepEqual(names, []string{"a", "b"}) {
			t.Fatalf("names = %v, ok = %v; want [a b], true", names, ok)
		}
	})

	t.Run("index order", func(t *testing.T) {
		// The keys are written out of order; the slice follows the indexes.
		values, ok, _ := GetSlice[int64](`{ [2] = 2, [1] = 1 }`)
		if !ok || !reflect.DeepEqual(values, []int64{1, 2}) {
			t.Fatalf("values = %v, ok = %v; want [1 2], true", values, ok)
		}
	})

	t.Run("not a slice", func(t *testing.T) {
		for _, path := range [][]any{
			{"mixed"},   // one element has the wrong type
			{"empty"},   // an empty table is not an array
			{"keyed"},   // not a pure array
			{"nothing"}, // nil
			{"missing"},
			{"ports", 1}, // an element, not a table
		} {
			values, ok, err := GetSlice[int64](src, path...)
			if err != nil {
				t.Fatalf("unexpected error for %v: %s", path, err)
			}
			if ok || values != nil {
				t.Fatalf("GetSlice[int64](%v) = %v, %v; want nil, false", path, values, ok)
			}
		}
	})
}
