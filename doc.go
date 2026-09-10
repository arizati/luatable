/*
Package luatable implements a dependency-free parser for Lua table
constructors, covering the syntax accepted by Lua 5.1 through Lua 5.4.

# Overview

luatable turns a Lua table constructor such as

	local config = {
	    name = "demo",        -- identifier key
	    ["timeout"] = 30,     -- bracket key
	    [1] = "first",        -- numeric key
	    [true] = "flag",      -- boolean key
	    { 1, 2, 3 },          -- positional (array) field
	}

into a generic Go value. The leading "local config =" part is ordinary Lua
code and is not part of a table constructor; luatable parses the "{" ... "}"
expression. Use Parser.AllowReturnPrefix (or the ParseModule helpers) when the
input is a Lua module file of the form "return { ... }".

# Returned values

A parsed table is represented as an interface{} holding one of:

	nil                  for Lua nil
	bool                 for Lua true / false
	int64                for Lua integer literals
	float64              for Lua float literals
	string               for Lua string literals
	[]interface{}        for a pure array table (keys are exactly 1..n)
	map[string]interface{} for any other table

When a table is not a pure array, its array part is exposed in the map using
decimal string keys ("1", "2", ...), mirroring how JSON-like formats represent
arrays inside objects. An empty table decodes to an empty map.

For full fidelity (exact key types and insertion order) use Parser.ParseTable
and work with the returned *Table; Table.Interface converts it back into the
generic representation described above.

# Supported syntax

  - array part, hash part, and mixed tables
  - nested table constructors
  - identifier keys ("name = value")
  - bracket keys ("[expr] = value") with string, integer, float and boolean keys
  - short strings with single or double quotes and all Lua escape sequences
  - long strings ("[[...]]", "[=[...]=]") with arbitrary levels
  - decimal, hexadecimal and hexadecimal-float numbers, including exponents
  - unary minus and parenthesized literal expressions
  - line comments, block comments and long-bracket comments
  - "," and ";" field separators, including trailing separators

# Unsupported syntax

Values must be literals, nested tables, unary minus or parenthesized literals.
Variable references, function calls, arithmetic and concatenation expressions
(for example "math.huge" or "1 + 2") are rejected with a *SyntaxError that
carries the byte offset, line and column of the offending construct.

# Example

	value, err := luatable.Parse(`{ name = "demo", items = { 1, 2, 3 } }`)
	if err != nil {
	    log.Fatal(err)
	}
	table := value.(map[string]interface{})
	fmt.Println(table["name"])        // demo
	fmt.Println(table["items"].([]interface{})) // [1 2 3]
*/
package luatable
