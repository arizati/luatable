package luatable

import (
	"math"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
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

		// math.MinInt64 is written in decimal, like every other integer.
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

// luaSupportsHexFloat reports whether the interpreter can read a hexadecimal
// float literal, which Lua 5.2 added and Lua 5.1 lacks. Probing is used instead
// of parsing the version string, because LuaJIT reports "Lua 5.1" yet does
// support the syntax.
func luaSupportsHexFloat(lua string) bool {
	out, err := exec.Command(lua, "-e",
		`local f = (loadstring or load)("return 0x1.8p1") io.write(f and "yes" or "no")`).Output()
	return err == nil && strings.TrimSpace(string(out)) == "yes"
}

// luaSupportsHexFormat reports whether string.format understands "%a", the
// conversion the oracle reads an exact double back with. C99 added it, but some
// C libraries, Microsoft's among them, do not implement it.
func luaSupportsHexFormat(lua string) bool {
	out, err := exec.Command(lua, "-e", `io.write(string.format("%a", 1.5))`).Output()
	if err != nil {
		return false
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	return err == nil && f == 1.5
}

// luaHexDump evaluates every literal listed in the file named by the first
// argument and prints "literal<TAB>%.17g<TAB>%a" for it. The "%a" rendering is
// exact, so reading that column back cannot be blurred by an interpreter whose
// "%.17g" carries a rounding of its own.
const luaHexDump = `
local loadstr = loadstring or load

for line in io.lines(arg[1]) do
  local f = loadstr("return " .. line)
  if f == nil then
    print(line .. "\tLOAD-ERROR")
  else
    local v = f()
    print(string.format("%s\t%.17g\t%a", line, v, v))
  end
end
`

// hexFloatLiterals returns a deterministic set of hexadecimal float literals
// whose mantissas are long enough to require rounding, with exponents that
// overflow and underflow the float64 range as well.
func hexFloatLiterals(n int) []string {
	rng := rand.New(rand.NewPCG(0xfeed, 0xbeef))

	lits := make([]string, 0, n)
	for range n {
		var b strings.Builder
		b.WriteString("0x")
		for range 1 + rng.IntN(14) {
			b.WriteByte("0123456789abcdef"[rng.IntN(16)])
		}
		b.WriteByte('.')
		for range 1 + rng.IntN(20) {
			b.WriteByte("0123456789abcdef"[rng.IntN(16)])
		}
		b.WriteString("p")
		b.WriteString(strconv.Itoa(rng.IntN(2200) - 1100))
		lits = append(lits, b.String())
	}
	return lits
}

// formatLuaNumber renders f the way C's "%.17g" does, so that a failure message
// shows the value the way an interpreter would have printed it. The comparison
// itself does not depend on this rendering: it is made on the exact "%a"
// rendering instead.
func formatLuaNumber(f float64) string {
	switch {
	case math.IsInf(f, 1):
		return "inf"
	case math.IsInf(f, -1):
		return "-inf"
	default:
		return strconv.FormatFloat(f, 'g', 17, 64)
	}
}

// ulpDiff reports how many representable float64 values lie between a and b.
// It quantifies a deviation in failure messages.
func ulpDiff(a, b float64) uint64 {
	ua, ub := orderedBits(a), orderedBits(b)
	if ua >= ub {
		return ua - ub
	}
	return ub - ua
}

// orderedBits maps a float64 bit pattern to a uint64 that grows with the
// value it denotes, so that the ordering of doubles becomes the ordering of
// integers. That also places -0 next to +0.
func orderedBits(f float64) uint64 {
	b := math.Float64bits(f)
	if b&(1<<63) != 0 {
		return ^b
	}
	return b | 1<<63
}

// TestULPDiff pins the semantics failure messages rely on: the count is
// symmetric, and adjacent doubles are one apart, including the pairs around
// zero and the infinities.
func TestULPDiff(t *testing.T) {
	cases := []struct {
		name string
		a, b float64
		want uint64
	}{
		{"equal", 1, 1, 0},
		{"next up", 1, math.Nextafter(1, 2), 1},
		{"next down", 1, math.Nextafter(1, 0), 1},
		{"symmetric", math.Nextafter(1, 2), 1, 1},
		{"signed zero", 0, math.Copysign(0, -1), 1},
		{"zero to smallest subnormal", 0, math.SmallestNonzeroFloat64, 1},
		{"largest finite to infinity", math.MaxFloat64, math.Inf(1), 1},
		{"negative infinity to smallest finite", math.Inf(-1), -math.MaxFloat64, 1},
		{"one to two", 1, 2, 1 << 52},
	}
	for _, tc := range cases {
		if got := ulpDiff(tc.a, tc.b); got != tc.want {
			t.Fatalf("ulpDiff(%v, %v) [%s] = %d; want %d", tc.a, tc.b, tc.name, got, tc.want)
		}
	}
}

// TestHexFloatMatchesLua compares the hexadecimal float conversion with a real
// interpreter. TestHexFloatMatchesStrconv only pins which conversion is used;
// this is the oracle that says the choice is the right one, and it is what the
// conversion is documented against.
//
// Every literal is read back as "%a", which renders the double an interpreter
// holds exactly, so the two sides are compared bit for bit and no tolerance is
// needed. That matters for LuaJIT, which converts and formats with its own code
// instead of the platform strtod and printf: distribution builds of LuaJIT
// 2.1.0~beta3 round "%.17g" ties away from zero instead of to even, which would
// shift a decimal rendering by one ULP even though the double behind it is
// unchanged. The "%.17g" column is printed for the failure messages only.
//
// An interpreter that cannot read hexadecimal floats at all (Lua 5.1) is
// skipped, and so is one whose string.format cannot print them, and so is the
// whole test when no interpreter is installed or in -short mode.
func TestHexFloatMatchesLua(t *testing.T) {
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

	lits := hexFloatLiterals(2000)
	list := strings.Join(lits, "\n") + "\n"

	for _, lua := range interpreters {
		base := filepath.Base(lua)
		t.Run(base, func(t *testing.T) {
			if !luaSupportsHexFloat(lua) {
				t.Skip("the interpreter cannot read hexadecimal float literals")
			}
			if !luaSupportsHexFormat(lua) {
				t.Skip("the interpreter cannot print hexadecimal floats with %a")
			}

			dir := t.TempDir()
			literals := filepath.Join(dir, "literals.txt")
			if err := os.WriteFile(literals, []byte(list), 0o600); err != nil {
				t.Fatalf("cannot write %s: %s", literals, err)
			}
			script := filepath.Join(dir, "dump.lua")
			if err := os.WriteFile(script, []byte(luaHexDump), 0o600); err != nil {
				t.Fatalf("cannot write %s: %s", script, err)
			}

			out, err := exec.Command(lua, script, literals).Output()
			if err != nil {
				t.Fatalf("%s failed: %s", base, err)
			}

			lines := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
			if len(lines) != len(lits) {
				t.Fatalf("got %d lines of output; want %d", len(lines), len(lits))
			}

			for i, line := range lines {
				lit, rest, ok := strings.Cut(line, "\t")
				if !ok || lit != lits[i] {
					t.Fatalf("unexpected output on line %d: %q", i, line)
				}
				luaValue, luaHex, ok := strings.Cut(rest, "\t")
				if !ok {
					t.Fatalf("unexpected output on line %d: %q", i, line)
				}

				value, err := parseLuaNumber(lit)
				if err != nil {
					t.Fatalf("parseLuaNumber(%q) failed: %s", lit, err)
				}
				f, ok := value.(float64)
				if !ok {
					t.Fatalf("parseLuaNumber(%q) = %T; want float64", lit, value)
				}

				// "%a" is exact, so it names the double the interpreter held
				// down to the bit. The "%.17g" column only makes the message
				// readable, because an interpreter may round it differently.
				luaF, err := strconv.ParseFloat(luaHex, 64)
				if err != nil {
					t.Fatalf("%s printed %q for %q: %s", base, luaHex, lit, err)
				}
				if math.Float64bits(f) != math.Float64bits(luaF) {
					t.Fatalf("parseLuaNumber(%q) = %s (%x); %s says %s (%x, %d ULP apart)",
						lit, formatLuaNumber(f), f, base, luaValue, luaF, ulpDiff(f, luaF))
				}
			}
		})
	}
}
