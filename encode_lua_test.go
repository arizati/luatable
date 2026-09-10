package luatable

import (
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// luaInterpreters lists the interpreter names probed by findLua, most recent
// first. Any of them understands the subset the encoder emits.
var luaInterpreters = []string{"lua", "lua5.5", "lua5.4", "lua5.3", "lua5.2", "lua5.1", "luajit"}

// findLua returns the path of a Lua interpreter, or "" when none is installed.
func findLua() string {
	for _, name := range luaInterpreters {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	return ""
}

// luaRunner loads the two generated files, calls them and checks that both
// produce a table. It deliberately avoids any library function that differs
// between Lua versions.
const luaRunner = `
local value = assert(loadfile("value.lua"))
local module = assert(loadfile("module.lua"))

local v = value()
assert(type(v) == "table", "value.lua did not evaluate to a table, got " .. type(v))

local m = module()
assert(type(m) == "table", "module.lua did not return a table, got " .. type(m))
`

// TestMarshalOutputIsValidLua feeds the encoder output to a real Lua
// implementation and checks that it compiles and evaluates to a table.
//
// This is the only test that does not use this package as its own oracle. The
// parser is deliberately lenient (see Parser.StrictKeywords) and therefore
// accepts some literals that real Lua rejects, so an in-process round trip
// cannot prove that the encoder emits valid Lua; an external interpreter can.
//
// The check is skipped when no interpreter is installed and in -short mode.
func TestMarshalOutputIsValidLua(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping the external interpreter check in short mode")
	}

	lua := findLua()
	if lua == "" {
		t.Skipf("no Lua interpreter found (tried %v)", luaInterpreters)
	}

	deep := any(int64(1))
	for range 20 {
		deep = []any{deep}
	}

	cases := []struct {
		name string
		v    any
	}{
		{"empty table", map[string]any{}},
		{"array", []any{int64(1), "two", 3.5, nil, true}},
		{"nested", map[string]any{"a": map[string]any{"b": []any{int64(1), int64(2)}}}},
		{"deep nesting", deep},

		// The regression guard for the reserved word fix: every one of these
		// keys is rejected by real Lua when written bare.
		{"reserved word keys", map[string]any{
			"and": int64(1), "end": int64(2), "function": int64(3), "goto": int64(4),
			"global": int64(5), "local": int64(6), "nil": int64(7), "return": int64(8),
		}},

		{"non identifier keys", map[string]any{"max-connections": int64(1), "": int64(2), "中文": int64(3), "1": int64(4)}},

		// math.MinInt64 is the one value written in hexadecimal form.
		{"numbers", map[string]any{
			"min": int64(math.MinInt64), "max": int64(math.MaxInt64),
			"zero": int64(0), "one": 1.0, "negative": -0.25,
			"big": 1e21, "small": 1e-7, "hexfloat": 1.5,
		}},

		{"strings", []any{
			"quote \" here", `backslash \ here`, "line\nbreak\ttab",
			"\x00\x01\x7f\xff", "café 🤭", "]] -- [=[ \\a\\b",
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()

			compact, err := Marshal(tc.v)
			if err != nil {
				t.Fatalf("Marshal failed: %s", err)
			}
			indented, err := MarshalIndent(tc.v, "  ")
			if err != nil {
				t.Fatalf("MarshalIndent failed: %s", err)
			}
			module, err := MarshalModuleIndent(tc.v, "\t")
			if err != nil {
				t.Fatalf("MarshalModuleIndent failed: %s", err)
			}

			// Both the compact and the indented form are table expressions,
			// so they are assigned to a local.
			value := "local t = " + string(compact) + "\nreturn t\n"
			if err := os.WriteFile(filepath.Join(dir, "value.lua"), []byte(value), 0o600); err != nil {
				t.Fatalf("cannot write value.lua: %s", err)
			}
			if err := os.WriteFile(filepath.Join(dir, "module.lua"), module, 0o600); err != nil {
				t.Fatalf("cannot write module.lua: %s", err)
			}
			if err := os.WriteFile(filepath.Join(dir, "run.lua"), []byte(luaRunner), 0o600); err != nil {
				t.Fatalf("cannot write run.lua: %s", err)
			}

			cmd := exec.Command(lua, "run.lua")
			cmd.Dir = dir
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("Lua rejected the generated code: %s\n%s\ncompact:  %s\nindented: %s\nmodule:   %s",
					err, out, compact, indented, module)
			}
		})
	}
}

// TestMarshalOutputIsAcceptedByEveryLuaVersion checks the compact form of a
// value that exercises every emitting rule against every interpreter found on
// the machine, so that a version-specific regression shows up as a named
// subtest failure.
func TestMarshalOutputIsAcceptedByEveryLuaVersion(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping the external interpreter check in short mode")
	}

	var interpreters []string
	for _, name := range luaInterpreters {
		if path, err := exec.LookPath(name); err == nil {
			interpreters = append(interpreters, path)
		}
	}
	if len(interpreters) == 0 {
		t.Skipf("no Lua interpreter found (tried %v)", luaInterpreters)
	}

	value := map[string]any{
		"end": int64(1), "global": int64(2), "goto": int64(3),
		"list": []any{int64(1), 1.0, "x", "line\nbreak", "\xff"},
	}
	compact, err := Marshal(value)
	if err != nil {
		t.Fatalf("Marshal failed: %s", err)
	}

	for _, lua := range interpreters {
		t.Run(filepath.Base(lua), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "check.lua")
			script := "local t = " + string(compact) + "\nreturn t\n"
			if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
				t.Fatalf("cannot write %s: %s", path, err)
			}

			out, err := exec.Command(lua, path).CombinedOutput()
			if err != nil {
				t.Fatalf("%s rejected the generated code: %s\n%s\n%s", filepath.Base(lua), err, out, compact)
			}
		})
	}
}
