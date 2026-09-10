package luatable

import (
	"reflect"
	"sync"
	"testing"
)

func TestParserPoolGetPut(t *testing.T) {
	var pp ParserPool

	p := pp.Get()
	if p == nil {
		t.Fatal("Get returned nil")
	}
	pp.Put(p)

	// Configuring a parser and returning it to the pool must not leak the
	// configuration into the next borrower.
	p = pp.Get()
	p.MaxDepth = 7
	p.AllowReturnPrefix = true
	pp.Put(p)

	p = pp.Get()
	if p.MaxDepth != 0 || p.AllowReturnPrefix {
		t.Fatalf("Put must reset the parser configuration; got MaxDepth=%d AllowReturnPrefix=%v",
			p.MaxDepth, p.AllowReturnPrefix)
	}
	pp.Put(p)

	// Putting nil must be a no-op.
	pp.Put(nil)
}

func TestParserPoolConcurrentUse(t *testing.T) {
	var pp ParserPool

	const goroutines = 16
	const iterations = 200

	var wg sync.WaitGroup
	for range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range iterations {
				p := pp.Get()
				got, err := p.Parse(`{1, 2, name = "demo"}`)
				if err != nil {
					t.Errorf("unexpected error: %s", err)
					pp.Put(p)
					return
				}
				want := map[string]any{
					"1":    int64(1),
					"2":    int64(2),
					"name": "demo",
				}
				if !reflect.DeepEqual(got, want) {
					t.Errorf("unexpected value: %#v", got)
				}
				pp.Put(p)
			}
		}()
	}
	wg.Wait()
}

func TestHandyParseFunctions(t *testing.T) {
	t.Run("Parse", func(t *testing.T) {
		got, err := Parse(`{1, 2}`)
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
		if !reflect.DeepEqual(got, []any{int64(1), int64(2)}) {
			t.Fatalf("unexpected value: %#v", got)
		}
	})

	t.Run("ParseBytes", func(t *testing.T) {
		got, err := ParseBytes([]byte(`{a = 1}`))
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
		if !reflect.DeepEqual(got, map[string]any{"a": int64(1)}) {
			t.Fatalf("unexpected value: %#v", got)
		}
	})

	t.Run("MustParse", func(t *testing.T) {
		if got := MustParse(`{1}`); !reflect.DeepEqual(got, []any{int64(1)}) {
			t.Fatalf("unexpected value: %#v", got)
		}
	})

	t.Run("MustParseBytes", func(t *testing.T) {
		if got := MustParseBytes([]byte(`{1}`)); !reflect.DeepEqual(got, []any{int64(1)}) {
			t.Fatalf("unexpected value: %#v", got)
		}
	})

	t.Run("ParseTable", func(t *testing.T) {
		tbl, err := ParseTable(`{a = 1}`)
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
		if got, _ := tbl.Get("a"); got != int64(1) {
			t.Fatalf("unexpected value: %#v", got)
		}
	})

	t.Run("ParseTableBytes", func(t *testing.T) {
		tbl, err := ParseTableBytes([]byte(`{a = 1}`))
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
		if got, _ := tbl.Get("a"); got != int64(1) {
			t.Fatalf("unexpected value: %#v", got)
		}
	})

	t.Run("MustParseTable", func(t *testing.T) {
		if got, _ := MustParseTable(`{a = 1}`).Get("a"); got != int64(1) {
			t.Fatalf("unexpected value: %#v", got)
		}
	})

	t.Run("ParseModule", func(t *testing.T) {
		tbl, err := ParseModule(`return {a = 1}`)
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
		if got, _ := tbl.Get("a"); got != int64(1) {
			t.Fatalf("unexpected value: %#v", got)
		}
	})

	t.Run("ParseModuleBytes", func(t *testing.T) {
		tbl, err := ParseModuleBytes([]byte(`return {a = 1}`))
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
		if got, _ := tbl.Get("a"); got != int64(1) {
			t.Fatalf("unexpected value: %#v", got)
		}
	})

	t.Run("MustParseModule", func(t *testing.T) {
		if got, _ := MustParseModule(`return {a = 1}`).Get("a"); got != int64(1) {
			t.Fatalf("unexpected value: %#v", got)
		}
	})
}

func TestMustHelpersPanic(t *testing.T) {
	cases := []struct {
		name string
		fn   func()
	}{
		{"MustParse", func() { MustParse("{") }},
		{"MustParseBytes", func() { MustParseBytes([]byte("{")) }},
		{"MustParseTable", func() { MustParseTable("{") }},
		{"MustParseModule", func() { MustParseModule("return {") }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatalf("%s must panic on invalid input", tc.name)
				}
			}()
			tc.fn()
		})
	}
}

func TestHandyParsePropagatesErrors(t *testing.T) {
	if _, err := Parse(`{a = }`); err == nil {
		t.Fatal("expecting an error")
	}
	if _, err := ParseBytes([]byte(`{a = }`)); err == nil {
		t.Fatal("expecting an error")
	}
	if _, err := ParseTable(`{a = }`); err == nil {
		t.Fatal("expecting an error")
	}
	if _, err := ParseTableBytes([]byte(`{a = }`)); err == nil {
		t.Fatal("expecting an error")
	}
	if _, err := ParseModule(`return {a = }`); err == nil {
		t.Fatal("expecting an error")
	}
}
