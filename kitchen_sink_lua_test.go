package luatable

// This file holds the kitchen-sink test that needs a real Lua interpreter. The
// in-process tests live in kitchen_sink_test.go; keeping the interpreter
// oracles in a *_lua_test.go file makes the set of slow, externally dependent
// tests easy to audit and to skip.

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// kitchenSinkLuaAssertions checks the encoded fixture from inside a real Lua
// interpreter. It uses no library function that differs between Lua 5.1 and
// 5.5, so every version can run the same script, and it compares values that
// are exact in every version: integers that fit into a double, exactly
// representable floats, and strings.
const kitchenSinkLuaAssertions = `
assert(type(t) == "table", "the encoded fixture is not a table")

assert(t.int == 42)
assert(t.int_negative == -7)
assert(t.int_max == 9223372036854775807)
assert(t.float == 1.5)
assert(t.float_leading_dot == 0.5)
assert(t.float_trailing_dot == 1.0)
assert(t.float_exponent == 1000.0)
assert(t.float_small_exponent == 0.025)
assert(t.hex == 255)
assert(t.hex_wrapped == -1)
assert(t.hex_float == 3.0)

assert(t.double_quoted == "double")
assert(t.single_quoted == "single")
assert(t.escaped_tab == "a\tb")
assert(t.escaped_newline == "a\nb")
assert(t.escaped_carriage_return == "a\rb")
assert(t.escaped_backslash == "\\")
assert(t.escaped_quotes == "\"'")
assert(t.decimal_escapes == "ABC")
assert(t.hex_escapes == "AB")
assert(t.unicode_escapes == "HI")
assert(t.skipped_whitespace == "ab")
assert(t.long_string == "first\nsecond")
assert(t.leveled_long_string == "contains ]=] inside")

assert(t["spaced key"] == "quoted")
assert(t[7] == "integer key")
assert(t["7"] == "text seven")
assert(t[-3] == "negative integer key")
assert(t[1.5] == "float key")
assert(t[true] == "boolean key")
assert(t[false] == "other boolean key")
assert(t["end"] == "reserved word key")

assert(t.nothing == nil)
assert(t.yes == true)
assert(t.no == false)

assert(t.list[1] == 1 and t.list[2] == 2.5 and t.list[3] == "three" and t.list[4] == true and t.list[5] == nil)
assert(#t.semicolons == 3)
assert(next(t.empty) == nil)
assert(t.nested.one.two.three == "bottom")
assert(t.mixed[1] == "first" and t.mixed[2] == 2 and t.mixed.name == "mixed" and t.mixed[true] == "flag")
assert(t.parenthesized == 1)
assert(t.unary_minus == -2)
assert(#t.trailing_separator == 2)
assert(t.last == "done")
`

// TestKitchenSinkEncodedAcrossLuaVersions is the external oracle for the
// comprehensive fixture: the encoder output for it is loaded by every
// interpreter found on the machine, and the script asserts the decoded values
// there. The in-process round trip cannot replace this check, because the
// parser is deliberately lenient about reserved-word keys, while the encoder
// output has to compile on real Lua 5.1 through 5.5 and LuaJIT.
//
// The test is skipped when no interpreter is installed and in -short mode.
func TestKitchenSinkEncodedAcrossLuaVersions(t *testing.T) {
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

	table, err := ParseTable(readTestdata(t, "kitchen_sink.lua"))
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	encoded, err := Marshal(table)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	script := "local t = " + string(encoded) + "\n" + kitchenSinkLuaAssertions

	for _, lua := range interpreters {
		t.Run(filepath.Base(lua), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "check.lua")
			if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
				t.Fatalf("cannot write %s: %s", path, err)
			}

			out, err := exec.Command(lua, path).CombinedOutput()
			if err != nil {
				t.Fatalf("%s rejected the encoded fixture: %s\n%s\n%s",
					filepath.Base(lua), err, out, encoded)
			}
		})
	}
}
