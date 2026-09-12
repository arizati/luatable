/*
Package luatable reads and writes Lua table constructors. On input it accepts
the union of the syntax of Lua 5.1 through Lua 5.5; on output it emits a subset
that every one of those versions accepts.

# Overview

luatable turns a Lua table constructor such as

	local config = {
	    name = "demo",        -- identifier key
	    ["timeout"] = 30,     -- bracket key
	    [1] = "first",        -- numeric key
	    [true] = "flag",      -- boolean key
	    { 1, 2, 3 },          -- positional (array) field
	}

into a generic Go value, and turns such values back into table literals. The
leading "local config =" part is ordinary Lua code and is not part of a table
constructor; luatable parses the "{" ... "}" expression. Use
Parser.AllowReturnPrefix (or the ParseModule helpers) when the input is a Lua
module file of the form "return { ... }".

# Returned values

A parsed table is represented as an any holding one of:

	nil             for Lua nil
	bool            for Lua true / false
	int64           for Lua integer literals
	float64         for Lua float literals
	string          for Lua string literals
	[]any           for a pure array table (keys are exactly 1..n)
	map[string]any  for any other table
	Skipped         for a value the lenient parser could not decode

When a table is not a pure array, its array part is exposed in the map using
decimal string keys ("1", "2", ...), mirroring how JSON-like formats represent
arrays inside objects. An empty table decodes to an empty map.

For full fidelity (exact key types and insertion order) use Parser.ParseTable
and work with the returned *Table; Table.Interface converts it back into the
generic representation described above.

A numeric key and a text key with the same spelling ([1] and ["1"]) collapse
into one map key. When such keys are also assigned more than once, the generic
representation keeps the value of the last field in the source; this is the one
corner where it can differ from Table.Interface, which resolves the collapse by
entry order. Parse fills the generic representation directly, so it builds no
*Table on the way.

# Supported syntax

  - array part, hash part, and mixed tables
  - nested table constructors
  - identifier keys ("name = value")
  - bracket keys ("[expr] = value") with string, integer, float and boolean
    keys; NaN keys are rejected as they are in Lua, and infinite keys are
    rejected even though Lua accepts them, because the encoder could not write
    them back
  - short strings with single or double quotes and all Lua escape sequences,
    except that \u{XXX} is limited to the Unicode scalar range: Lua 5.4 itself
    accepts code points up to 0x7FFFFFFF, which have no single-rune Go
    representation
  - long strings ("[[...]]", "[=[...]=]") with arbitrary levels
  - decimal, hexadecimal and hexadecimal-float numbers (the latter since Lua
    5.2), including exponents; literals outside the float64 range (1e400,
    0x1p-1100) evaluate to ±Inf or to zero, as every reference implementation
    does, although the manual leaves float overflow unspecified
  - unary minus and parenthesized literal expressions
  - line comments, block comments and long-bracket comments
  - "," and ";" field separators, including trailing separators

# Unsupported syntax

Values must be literals, nested tables, unary minus or parenthesized literals.
Variable references, function calls, arithmetic and concatenation expressions
(for example "math.huge" or "1 + 2") are rejected with a *SyntaxError that
carries the byte offset, line and column of the offending construct.

Set Parser.Lenient to keep parsing instead. Such a value is consumed and
recorded as a Skipped, which keeps its position in an array, and a table key
that is not a literal makes the parser drop the whole field. Lenient mode is a
recovery mode for data files that mix literals with code; it is not a validation
mode. Only the errors that no recovery can pass are reported, positioned at the
value that was being skipped: an unterminated string or comment, a missing field
value ("{a = }"), and input that runs out before the value or the constructor
ends. A value that merely fails to decode as a literal is recorded as a Skipped
instead, even when it is incomplete ("-" or "(1"). Skipping works on tokens, so
recovery from input that is not valid Lua at all is best-effort: an unterminated
construct can leave a later field attached to the wrong index.

A leading UTF-8 byte order mark is likewise rejected as an unexpected
character: it is not part of Lua's lexical grammar (Lua 5.2 and later strip
one in loadfile, LuaJIT strips it everywhere, Lua 5.1 strips it nowhere), so
strip it before parsing files written by editors that add one.

# Reserved words

A table key that collides with a Lua reserved word has to be quoted: "{end = 1}"
is rejected by every Lua implementation, so the encoder writes {["end"] = 1}
instead. The set covers Lua 5.1 through Lua 5.5, which also means that "global"
(reserved since Lua 5.5) is quoted.

The parser is lenient by default and accepts the unquoted form, so that slightly
off-spec data files still load. Set Parser.StrictKeywords to reject reserved
words used as bare table keys, matching the reference implementation.

# Encoding

Marshal and its variants perform the reverse operation:

	out, err := luatable.Marshal(map[string]any{"name": "demo"})       // {name = "demo"}
	text, err := luatable.MarshalModule(map[string]any{"debug": true}) // return {debug = true}

The encoder only emits literals and nested tables, so its output is always
readable by Parse, and it sticks to forms that Lua 5.1 already understands.
Values outside its domain (a struct, a func, a NaN, an infinity, a uint64 that
does not fit into int64, ...) are rejected with an *EncodeError carrying the
path of the offending value, instead of producing invalid Lua. A Skipped value
is rejected as well; set Encoder.EmitSkipped to write its raw text back, which
is what turns a value parsed with Parser.Lenient into a round trip, at the cost
of no longer guaranteeing that the output is valid Lua. Note the
asymmetry around infinity: Parse produces ±Inf from literals outside the float64
range (1e400, 0x1p1024), while Marshal refuses to write one, because no literal
is specified to denote an infinity -- the manual leaves float overflow to the
implementation. math.MinInt64 is the second value whose round trip is not exact:
it is written in decimal, which Parse reads back as the float64 of the same
value, because no literal that every Lua version reads the same way denotes it
(0x8000000000000000 does on 5.3 and later, but not on 5.1, 5.2 or LuaJIT).

Pass a *Table to Marshal when exact key types and insertion order matter. A
map[string]any can only express string keys, so [1] and ["1"] become
indistinguishable, and its keys are emitted in sorted order so that the output
stays reproducible.

# Reading a single value

Get, GetAs and GetSlice read one value by path, without converting the rest of
the document:

	port, ok, err := luatable.GetAs[int64](src, "servers", 1, "port")

Each path element is a Lua key, so positional keys start at 1, and the typed
variants follow Lua's number model: an int64 is accepted as a float64, and a
float64 with an integral value in range is accepted as an int64. A lookup parses
in lenient mode (see Parser.Lenient) and accepts an optional "return" prefix,
because it is a query rather than a validation step; use Parse or ParseTable to
check the whole input. When several values are needed from the same input, parse
once with ParseTable and walk with Table.GetPath.

# Example

	value, err := luatable.Parse(`{ name = "demo", items = { 1, 2, 3 } }`)
	if err != nil {
	    log.Fatal(err)
	}
	table := value.(map[string]any)
	fmt.Println(table["name"])          // demo
	fmt.Println(table["items"].([]any)) // [1 2 3]

	out, err := luatable.Marshal(table)
	if err != nil {
	    log.Fatal(err)
	}
	fmt.Println(string(out))            // {items = {1,2,3},name = "demo"}
*/
package luatable
