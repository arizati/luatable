package luatable

// This file holds the lenient-mode tests that need a real Lua interpreter.
// The in-process tests live in lenient_test.go; keeping the interpreter
// oracles in a *_lua_test.go file makes the set of slow, externally dependent
// tests easy to audit and to skip.

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// dataDumperDriver loads the DataDumper fixture named by its first argument and
// writes a dump of a fixed sample table to stdout. The sample avoids closures
// and repeated table references, which DataDumper renders as a whole chunk
// ("local t = {...}" plus "return t") that this package does not parse: it only
// parses a table constructor. A function without upvalues and a C function are
// rendered inline, which is the shape the lenient parser has to recover from.
const dataDumperDriver = `
assert(loadfile(arg[1]))()

local sample = {
  name = "demo",
  count = 3,
  ratio = 0.5,
  flag = true,
  ["end"] = "reserved",
  list = { 1, 2, 3 },
  nested = { deep = { ok = true } },
  fn = function() return 42 end,
  builtin = string.format,
  huge = math.huge,
}

io.write(DataDumper(sample), "\n")
`

// luaSupportsDumper reports whether the interpreter can run the DataDumper
// fixture, which needs loadstring, string.dump and the debug library. The probe
// decides instead of the version string: Lua 5.2 still provides loadstring when
// it is built with its compatibility options, as the distribution builds are,
// while Lua 5.3 and later dropped it.
func luaSupportsDumper(lua string) bool {
	out, err := exec.Command(lua, "-e",
		`io.write(loadstring and string.dump and debug and "yes" or "no")`).Output()
	return err == nil && strings.TrimSpace(string(out)) == "yes"
}

// assertDumperData checks the values of the fixed sample that the driver dumps.
func assertDumperData(t *testing.T, table *Table) {
	t.Helper()

	for key, want := range map[string]any{
		"name":  "demo",
		"count": int64(3),
		"ratio": 0.5,
		"flag":  true,
		"end":   "reserved",
	} {
		got, ok := table.Get(key)
		if !ok {
			t.Fatalf("no entry for %q", key)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s = %#v; want %#v", key, got, want)
		}
	}

	nested, ok := table.Get("nested")
	if !ok {
		t.Fatal("no entry for nested")
	}
	inner, ok := nested.(*Table)
	if !ok {
		t.Fatalf("nested is %T; want *Table", nested)
	}
	deep, ok := inner.Get("deep")
	if !ok {
		t.Fatal("no entry for nested.deep")
	}
	deepest, ok := deep.(*Table)
	if !ok {
		t.Fatalf("nested.deep is %T; want *Table", deep)
	}
	if got, _ := deepest.Get("ok"); got != true {
		t.Fatalf("nested.deep.ok = %#v; want true", got)
	}

	// The values the parser cannot decode are recorded, not dropped: a C
	// function reference, a dumped function and the infinity, which
	// DataDumper renders as a bare "inf" that no Lua implementation loads.
	for key, want := range map[string]string{
		"builtin": "string.format",
		"huge":    "inf",
	} {
		if got := skippedAt(t, table, key).Text; got != want {
			t.Fatalf("%s.Text = %q; want %q", key, got, want)
		}
	}
	if fn := skippedAt(t, table, "fn").Text; !strings.HasPrefix(fn, "loadstring(") {
		t.Fatalf("fn.Text = %q; want a loadstring call", truncate(fn, 32))
	}
}

func TestLenientDataDumperOutput(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping the external interpreter check in short mode")
	}

	var interpreters []string
	for _, name := range luaInterpreters {
		if path, err := exec.LookPath(name); err == nil && luaSupportsDumper(path) {
			interpreters = append(interpreters, path)
		}
	}
	if len(interpreters) == 0 {
		t.Skipf("no interpreter that can run DataDumper found (tried %v)", luaInterpreters)
	}

	for _, lua := range interpreters {
		base := filepath.Base(lua)
		t.Run(base, func(t *testing.T) {
			driver := filepath.Join(t.TempDir(), "dump.lua")
			if err := os.WriteFile(driver, []byte(dataDumperDriver), 0o600); err != nil {
				t.Fatalf("cannot write %s: %s", driver, err)
			}

			out, err := exec.Command(lua, driver, filepath.Join("testdata", "dumper.lua")).CombinedOutput()
			if err != nil {
				t.Fatalf("%s failed: %s\n%s", base, err, out)
			}
			src := string(out)

			// The dump is Lua, but the strict parser rejects the function
			// values; each interpreter escapes the bytecode its own way,
			// raw control characters included.
			if _, err := Parse(src); err == nil {
				t.Fatal("the strict parser must reject a dump that contains loadstring calls")
			}

			var p Parser
			p.Lenient = true
			p.AllowReturnPrefix = true
			table, err := p.ParseTable(src)
			if err != nil {
				t.Fatalf("lenient parse failed: %s\ndump:\n%s", err, truncate(src, 200))
			}
			assertDumperData(t, table)

			// The lookups work on the dump as well.
			list, ok, err := GetSlice[int64](src, "list")
			if err != nil {
				t.Fatalf("unexpected error: %s", err)
			}
			if !ok || !reflect.DeepEqual(list, []int64{1, 2, 3}) {
				t.Fatalf("list = %v, ok = %v; want [1 2 3], true", list, ok)
			}

			// Writing the dump back is what Encoder.EmitSkipped is for: the
			// raw text of the skipped values survives, and the result is
			// readable again.
			encoded, err := (&Encoder{EmitSkipped: true}).Marshal(table)
			if err != nil {
				t.Fatalf("cannot write the dump back: %s", err)
			}
			back, err := p.ParseTable(string(encoded))
			if err != nil {
				t.Fatalf("the written-back dump is not parseable: %s\n%s", err, truncate(string(encoded), 200))
			}
			assertDumperData(t, back)

			// The dumped function survives the write-back byte for byte,
			// control characters, escapes and all.
			if got, want := skippedAt(t, back, "fn").Text, skippedAt(t, table, "fn").Text; got != want {
				t.Fatalf("fn.Text changed through the write-back:\n got %q\nwant %q",
					truncate(got, 32), truncate(want, 32))
			}
		})
	}
}
