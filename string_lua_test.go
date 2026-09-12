package luatable

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestZEscapeMatchesLua is the external oracle for the "\z" escape: every
// literal is evaluated by a real interpreter and by this package, and both
// results must be the same byte string.
//
// The escape was added by Lua 5.2, and LuaJIT supports it while reporting
// "Lua 5.1", so support is probed behaviourally instead of being derived from
// the version string. Interpreters that reject the escape are skipped, which
// leaves Lua 5.1 out; the parser still accepts the escape there, because it
// accepts the union of the syntax of Lua 5.1 through 5.5.
//
// The check is skipped when no interpreter is installed and in -short mode.
func TestZEscapeMatchesLua(t *testing.T) {
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

	cases := []struct {
		name    string
		literal string
		want    string
	}{
		{"line feed", "\"x\\z\ny\"", "xy"},
		{"carriage return line feed", "\"x\\z\r\ny\"", "xy"},
		{"spaces and tabs", "\"x\\z \t y\"", "xy"},
		{"up to the closing quote", "\"x\\z \n \"", "x"},
		{"before another escape", "\"x\\z \\n y\"", "x\n y"},
	}

	for _, lua := range interpreters {
		t.Run(filepath.Base(lua), func(t *testing.T) {
			dir := t.TempDir()

			if !luaSupportsZEscape(t, lua, dir) {
				t.Skip("the interpreter does not support the \\z escape")
			}

			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					// The interpreter's own evaluation of the literal.
					script := filepath.Join(dir, "value.lua")
					content := "local s = " + tc.literal + "\nio.write(s)\n"
					if err := os.WriteFile(script, []byte(content), 0o600); err != nil {
						t.Fatalf("cannot write %s: %s", script, err)
					}
					out, err := exec.Command(lua, script).Output()
					if err != nil {
						t.Fatalf("%s rejected %s: %s", filepath.Base(lua), tc.literal, err)
					}
					if string(out) != tc.want {
						t.Fatalf("%s evaluates %s to %q; want %q", filepath.Base(lua), tc.literal, out, tc.want)
					}

					// This package must agree with the interpreter.
					got, err := Parse("{a = " + tc.literal + "}")
					if err != nil {
						t.Fatalf("unexpected parse error: %s", err)
					}
					if value := got.(map[string]any)["a"]; value != tc.want {
						t.Fatalf("Parse(%q) = %q; want %q", tc.literal, value, tc.want)
					}
				})
			}
		})
	}
}

// luaSupportsZEscape reports whether the interpreter can read the "\z" escape
// across a line break. The probe writes a small script instead of using -e, so
// that the literal reaches the interpreter exactly as written.
func luaSupportsZEscape(t *testing.T, lua, dir string) bool {
	t.Helper()

	probe := filepath.Join(dir, "probe.lua")
	if err := os.WriteFile(probe, []byte("local s = \"x\\z\n y\"\nio.write(s)\n"), 0o600); err != nil {
		t.Fatalf("cannot write %s: %s", probe, err)
	}

	out, err := exec.Command(lua, probe).Output()
	return err == nil && strings.TrimSpace(string(out)) == "xy"
}

// TestLineContinuationsMatchLua is the external oracle for the
// backslash-newline escape. Every interpreter must agree with this package:
// LF, CR, CRLF and LFCR each continue the string with one newline, two
// separate escapes produce two newlines, and a second raw newline after a
// single backslash is an unterminated string.
//
// All supported versions accept the escape, so unlike the "\z" oracle there is
// no probe and no skip; the negative cases check that the interpreter rejects
// the same input this package rejects.
func TestLineContinuationsMatchLua(t *testing.T) {
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

	cases := []struct {
		name    string
		literal string
		want    string
		wantErr bool
	}{
		{"line feed", "\"a\\\nb\"", "a\nb", false},
		{"carriage return", "\"a\\\rb\"", "a\nb", false},
		{"carriage return line feed", "\"a\\\r\nb\"", "a\nb", false},
		{"line feed carriage return", "\"a\\\n\rb\"", "a\nb", false},
		{"two continuations", "\"a\\\n\\\nb\"", "a\n\nb", false},
		{"two line feeds after one backslash", "\"a\\\n\nb\"", "", true},
		{"two carriage returns after one backslash", "\"a\\\r\rb\"", "", true},
		{"line feed then a carriage return line feed", "\"a\\\n\r\nb\"", "", true},
	}

	for _, lua := range interpreters {
		t.Run(filepath.Base(lua), func(t *testing.T) {
			dir := t.TempDir()

			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					// The interpreter's own verdict on the literal.
					script := filepath.Join(dir, "value.lua")
					content := "local s = " + tc.literal + "\nio.write(s)\n"
					if err := os.WriteFile(script, []byte(content), 0o600); err != nil {
						t.Fatalf("cannot write %s: %s", script, err)
					}
					out, luaErr := exec.Command(lua, script).Output()

					// This package must reach the same verdict.
					got, parseErr := Parse("{a = " + tc.literal + "}")

					if tc.wantErr {
						if luaErr == nil {
							t.Fatalf("%s accepts %q; want a syntax error", filepath.Base(lua), tc.literal)
						}
						if parseErr == nil {
							t.Fatalf("Parse accepts %q; want a syntax error", tc.literal)
						}
						return
					}

					if luaErr != nil {
						t.Fatalf("%s rejects %q: %s", filepath.Base(lua), tc.literal, luaErr)
					}
					if string(out) != tc.want {
						t.Fatalf("%s evaluates %q to %q; want %q", filepath.Base(lua), tc.literal, out, tc.want)
					}
					if parseErr != nil {
						t.Fatalf("Parse(%q) failed: %s", tc.literal, parseErr)
					}
					if value := got.(map[string]any)["a"]; value != tc.want {
						t.Fatalf("Parse(%q) = %q; want %q", tc.literal, value, tc.want)
					}
				})
			}
		})
	}
}
