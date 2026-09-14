package luatable

import (
	"errors"
	"math"
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

func TestAs(t *testing.T) {
	// The source holds a value the lenient parser records as a Skipped, so it
	// is parsed the way a lookup parses it.
	var p Parser
	p.Lenient = true
	table, err := p.ParseTable(scalarSrc)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	t.Run("int64", func(t *testing.T) {
		// The two results of Get are the two arguments of As.
		if port, ok := As[int64](table.Get("port")); !ok || port != 8080 {
			t.Fatalf("port = %d, ok = %v; want 8080, true", port, ok)
		}

		// A float with an integral value converts, as math.tointeger does.
		if whole, ok := As[int64](table.Get("whole")); !ok || whole != 3 {
			t.Fatalf("whole = %d, ok = %v; want 3, true", whole, ok)
		}

		for _, key := range []string{"ratio", "name", "flag", "nothing", "skipped"} {
			if value, ok := As[int64](table.Get(key)); ok || value != 0 {
				t.Fatalf("As[int64](%s) = %d, %v; want 0, false", key, value, ok)
			}
		}

		// A missing field fails the conversion through the flag the lookup
		// returned, not through the value, which is nil either way.
		if value, ok := As[int64](table.Get("nope")); ok || value != 0 {
			t.Fatalf("As[int64](nope) = %d, %v; want 0, false", value, ok)
		}
	})

	t.Run("float64", func(t *testing.T) {
		if ratio, ok := As[float64](table.Get("ratio")); !ok || ratio != 2.5 {
			t.Fatalf("ratio = %v, ok = %v; want 2.5, true", ratio, ok)
		}
		if port, ok := As[float64](table.Get("port")); !ok || port != 8080 {
			t.Fatalf("port = %v, ok = %v; want 8080, true", port, ok)
		}
		if name, ok := As[float64](table.Get("name")); ok || name != 0 {
			t.Fatalf("name = %v, ok = %v; want 0, false", name, ok)
		}
	})

	t.Run("string and bool", func(t *testing.T) {
		if name, ok := As[string](table.Get("name")); !ok || name != "demo" {
			t.Fatalf("name = %q, ok = %v; want \"demo\", true", name, ok)
		}
		if flag, ok := As[bool](table.Get("flag")); !ok || !flag {
			t.Fatalf("flag = %v, ok = %v; want true, true", flag, ok)
		}
		if name, ok := As[string](table.Get("port")); ok || name != "" {
			t.Fatalf("string of a number = %q, ok = %v; want \"\", false", name, ok)
		}
		if flag, ok := As[bool](table.Get("nothing")); ok || flag {
			t.Fatalf("bool of nil = %v, ok = %v; want false, false", flag, ok)
		}
	})

	t.Run("NaN and the infinities", func(t *testing.T) {
		// A table key can never be NaN or infinite, but a value can: they
		// are float64 values, so they convert to a float64 and never to an
		// int64.
		for _, v := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
			if f, ok := As[float64](v, true); !ok || math.Float64bits(f) != math.Float64bits(v) {
				t.Fatalf("As[float64](%v) = %v, %v; want %v, true", v, f, ok, v)
			}
			if n, ok := As[int64](v, true); ok || n != 0 {
				t.Fatalf("As[int64](%v) = %d, %v; want 0, false", v, n, ok)
			}
		}
	})

	t.Run("a nested table", func(t *testing.T) {
		nested := MustParseTable(`{ inner = { 1 } }`)
		if value, ok := As[int64](nested.Get("inner")); ok || value != 0 {
			t.Fatalf("As[int64](a table) = %d, %v; want 0, false", value, ok)
		}
	})

	t.Run("a value built by hand", func(t *testing.T) {
		// An integer of any Go width converts in either direction, which is
		// what makes As usable on a map the caller filled itself, where an
		// integer literal has the type int.
		for _, value := range []any{int(7), int32(7), uint8(7), uint(7)} {
			if n, ok := As[int64](value, true); !ok || n != 7 {
				t.Fatalf("int64 of %T = %d, ok = %v; want 7, true", value, n, ok)
			}
			if f, ok := As[float64](value, true); !ok || f != 7 {
				t.Fatalf("float64 of %T = %v, ok = %v; want 7, true", value, f, ok)
			}
		}

		// An integer that does not fit into an int64 is outside the range this
		// package represents an integer in, so neither direction accepts it.
		if n, ok := As[int64](uint64(math.MaxUint64), true); ok || n != 0 {
			t.Fatalf("uint64 max as int64 = %d, ok = %v; want 0, false", n, ok)
		}
		if f, ok := As[float64](uint64(math.MaxUint64), true); ok || f != 0 {
			t.Fatalf("uint64 max as float64 = %v, ok = %v; want 0, false", f, ok)
		}

		// A float64 is itself whatever its magnitude; only a conversion to an
		// int64 has to be in range.
		if f, ok := As[float64](1e300, true); !ok || f != 1e300 {
			t.Fatalf("1e300 as float64 = %v, ok = %v; want 1e300, true", f, ok)
		}
		if n, ok := As[int64](1e300, true); ok || n != 0 {
			t.Fatalf("1e300 as int64 = %d, ok = %v; want 0, false", n, ok)
		}
	})

	t.Run("a failed lookup", func(t *testing.T) {
		if value, ok := As[string](nil, false); ok || value != "" {
			t.Fatalf("As[string](nil, false) = %q, %v; want \"\", false", value, ok)
		}
	})
}

func TestAsSlice(t *testing.T) {
	// The generic representation hands arrays out as []any.
	value, err := Parse(`{
		ports = { 8080, 9090 },
		names = { "a", "b" },
		mixed = { 1, "x" },
	}`)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	generic := value.(map[string]any)

	t.Run("int64", func(t *testing.T) {
		ports, ok := AsSlice[int64](generic["ports"].([]any), true)
		if !ok || !reflect.DeepEqual(ports, []int64{8080, 9090}) {
			t.Fatalf("ports = %v, ok = %v; want [8080 9090], true", ports, ok)
		}
	})

	t.Run("float64 from integers", func(t *testing.T) {
		ports, ok := AsSlice[float64](generic["ports"].([]any), true)
		if !ok || !reflect.DeepEqual(ports, []float64{8080, 9090}) {
			t.Fatalf("ports = %v, ok = %v; want [8080 9090], true", ports, ok)
		}
	})

	t.Run("strings", func(t *testing.T) {
		names, ok := AsSlice[string](generic["names"].([]any), true)
		if !ok || !reflect.DeepEqual(names, []string{"a", "b"}) {
			t.Fatalf("names = %v, ok = %v; want [a b], true", names, ok)
		}
	})

	t.Run("an array of a rich table", func(t *testing.T) {
		table := MustParseTable(`{ ports = { 8080, 9090 } }`)
		element, _ := table.Get("ports")

		// Table.Array is the rich route to the []any AsSlice takes.
		ports, ok := AsSlice[int64](element.(*Table).Array(), true)
		if !ok || !reflect.DeepEqual(ports, []int64{8080, 9090}) {
			t.Fatalf("ports = %v, ok = %v; want [8080 9090], true", ports, ok)
		}
	})

	t.Run("no conversion", func(t *testing.T) {
		cases := []struct {
			name     string
			elements []any
			ok       bool
		}{
			{"a failed lookup", []any{int64(1)}, false},
			{"nil, which is not an array", nil, true},
			{"a mixed array", generic["mixed"].([]any), true},
			{"a nil element", []any{nil}, true},
			{"a table element", []any{MustParseTable(`{1}`)}, true},
			{"a skipped element", []any{Skipped{}}, true},
		}

		for _, tc := range cases {
			values, ok := AsSlice[int64](tc.elements, tc.ok)
			if ok || values != nil {
				t.Fatalf("%s: AsSlice[int64] = %v, %v; want nil, false", tc.name, values, ok)
			}
		}
	})

	t.Run("an empty array", func(t *testing.T) {
		values, ok := AsSlice[int64]([]any{}, true)
		if !ok || values == nil || len(values) != 0 {
			t.Fatalf("AsSlice[int64]([]) = %v, %v; want [], true", values, ok)
		}
	})
}

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
