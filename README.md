# luatable

A pure Go reader and writer for Lua table constructors (Lua 5.1 – 5.5), treated
as a data format rather than as code. No third-party dependencies, no code
generation, no reflection magic — just parse a table into a plain Go value, or
generate one from Go data.

A table constructor is the Lua equivalent of a JSON document: the notation a Lua
program uses to write down configuration, fixtures and payloads. This package
treats it that way. It lexes and parses the constructor into plain Go values the
way a JSON decoder reads its input, and it evaluates nothing: there is no Lua VM,
no bytecode and no code execution. Every value must be a literal or a nested
table (`nil`, booleans, integers, floats, strings, tables), while anything that
would need evaluation — variable references, function calls, arithmetic,
concatenation — is rejected with a positioned syntax error.

That distinction is the point. Reading such a file from inside Lua means
executing it — `dofile`, `require`, or `load`/`loadstring` plus a call — which
runs whatever the input contains, deliberately or not, whereas `luatable`
extracts the data from the text alone. `Marshal` goes the other way and writes
Go data back as a table literal for a real Lua program to load. Table syntax is
not a portable interchange format — JSON may serve better when no Lua program is
involved — but when the other side is Lua, it keeps the data readable on both
sides.

```go
value, err := luatable.Parse(`{ name = "demo", items = { 1, 2, 3 } }`)
if err != nil {
    log.Fatal(err)
}
table := value.(map[string]any)
fmt.Println(table["name"])          // demo
fmt.Println(table["items"].([]any)) // [1 2 3]
```

## Features

* Array part, hash part and mixed tables, including nested tables.
* Identifier keys (`name = value`), bracket keys (`[expr] = value`), string
  keys, integer / float / boolean keys.
* Short strings (`'...'`, `"..."`) with every Lua escape sequence, including
  `\ddd`, `\xHH`, `\z`, `\u{XXXX}` and line continuations (`\` before LF, CR,
  CRLF or LFCR).
* Long strings (`[[...]]`, `[=[...]=]`, arbitrary levels).
* Decimal, hexadecimal and hexadecimal-float literals (Lua 5.2+), including
  exponent forms.
* Unary minus and parenthesized literal expressions.
* Line comments, block comments and long-bracket comments.
* `,` and `;` field separators, including trailing separators.
* Optional `return` prefix for Lua module files (`return { ... }`).
* Precise error positions: byte offset, line and column.
* Optional rich representation preserving exact key types and insertion order.
* **Path lookup**: `Get`, `GetAs[T]` and `GetSlice[T]` read one value without
  converting the whole document.
* **Lenient recovery**: `Parser.Lenient` records a value it cannot decode as a
  `Skipped` and keeps the surrounding data, so a file that mixes literals with
  code still loads.
* Safe against hostile input: bounded nesting depth, no panics (fuzz-tested),
  and nothing is ever evaluated. Only the strict mode also guarantees that every
  value is a literal.
* **Generation**: `Marshal` writes Go values back as Lua table literals that the
  parser reads back, with deterministic ordering and byte-exact string escaping.
* Reserved-word-safe keys: `Marshal` quotes every key that Lua reserves, so
  `end` becomes `["end"]` and the output compiles on Lua 5.1 through 5.5.

## Install

```bash
go get github.com/arizati/luatable
```

## Returned values

`luatable.Parse` returns an `any` holding one of:

| Lua value | Go value |
| --- | --- |
| `nil` | `nil` |
| `true` / `false` | `bool` |
| integer literal | `int64` |
| float literal | `float64` |
| string literal | `string` |
| array table (keys are exactly `1..n`) | `[]any` |
| any other table | `map[string]any` |

A table that is not a pure array exposes its array part through decimal string
keys (`"1"`, `"2"`, …), mirroring how JSON-like formats represent arrays inside
objects. An empty table decodes to an empty map.

In lenient mode a value the parser cannot decode appears as a `Skipped` instead;
see [Values that are not literals](#values-that-are-not-literals).

```go
luatable.Parse(`{ "a", "b", "c" }`)          // []any{"a", "b", "c"}
luatable.Parse(`{ 1, 2, name = "demo" }`)    // map[string]any{"1": 1, "2": 2, "name": "demo"}
luatable.Parse(`{}`)                         // map[string]any{}
```

## Rich representation

When exact key types and ordering matter, use `ParseTable`. It returns a
`*Table` whose entries preserve the original key types and insertion order.
Nested tables are stored as `*Table` too, so fidelity is available at every
level.

```go
table, err := luatable.ParseTable(`{ 1, kind = "demo", [true] = "yes" }`)
if err != nil {
    log.Fatal(err)
}

for _, entry := range table.Entries() {
    fmt.Printf("%v (%T) = %v\n", entry.Key, entry.Key, entry.Value)
}

// 1 (int64) = 1
// kind (string) = demo
// true (bool) = yes
```

`Table` API:

| Method | Description |
| --- | --- |
| `Len() int` | number of entries |
| `Entries() []Entry` | ordered copy of all entries |
| `Get(key any) (any, bool)` | value for a key (`1` and `1.0` are equivalent) |
| `IsArray() bool` | whether the keys are exactly `1..n` |
| `Array() []any` | array view, or `nil` when not an array |
| `Map() map[string]any` | generic map view |
| `Interface() any` | `[]any` for arrays, `map[string]any` otherwise |
| `String() string` | Lua-like text, for debugging |

Use `luatable.ToInterface(v)` (or `Table.Interface`) to recursively convert a
rich table into the generic representation.

`Parse` does not build a `*Table` and then convert it: it fills the generic
representation directly, so it is the cheaper of the two and allocates no
intermediate table (see `BenchmarkParse` against `BenchmarkParseTable`). Reach
for `ParseTable` when exact key types or entry order are needed.

## Values that are not literals

A Lua data file sometimes mixes data with code: a dumped table may hold a
function reference or a `loadstring(...)` call next to its numbers and strings.
`Parser.Lenient` keeps parsing such a file instead of rejecting it. A field value
that cannot be decoded as a literal or a nested table is consumed and recorded
as a `Skipped` value, which keeps its place in an array, and a table key that is
not a literal makes the parser drop the whole field.

```go
var p luatable.Parser
p.Lenient = true
table, err := p.ParseTable(`{ id = 1, fn = loadstring("\27LJ") }`)
if err != nil {
    log.Fatal(err)
}

id, _ := table.Get("id") // int64(1)
fn, _ := table.Get("fn") // luatable.Skipped{Text: `loadstring("\27LJ")`}
```

`Skipped.Text` is the raw source text, a substring of the input, so it costs
nothing to keep and it shows what the field was. `Skipped.Offset` is positional
metadata: it differs between two parses of the same data, so compare `Text` when
comparing values.

Lenient mode is a recovery mode, not a validation mode. It evaluates nothing,
and only the errors that no recovery can pass are reported, positioned at the
value that was being skipped: an unterminated string or comment, a missing field
value (`{a = }`), and input that runs out before the value or the constructor
ends. A value that merely fails to decode as a literal is recorded as `Skipped`
rather than dropped, even when it is incomplete (`-` or `(1`). Skipping works on
tokens, so recovery from input that is not valid Lua at all is best-effort: an
unterminated construct can leave a later field attached to the wrong index. Use
the default strict mode when a file has to contain nothing but literals.

`Marshal` rejects a `Skipped` value, because writing the raw text back could
emit code or text that is not valid Lua. `Encoder.EmitSkipped` lifts that check
for text you trust, which is what lets a dump survive a round trip.

## Reading a single value

`Get`, `GetAs` and `GetSlice` read one value by path, without converting the rest
of the document:

```go
port, ok, err := luatable.GetAs[int64](src, "servers", 1, "port")
if err != nil {
    return err // *SyntaxError: src is not a table constructor
}
if !ok {
    return fmt.Errorf("servers[1].port is missing or not an integer")
}
```

Each path element is a Lua key: a string selects a string key, an integer
selects a positional field, a boolean selects a boolean key. Positional keys
start at 1, as they do in Lua, and `1`, `int64(1)` and `1.0` select the same
entry. A field whose value is `nil` is present, so a lookup reports `true` with a
`nil` value; a missing path is not an error.

| Function | Result |
| --- | --- |
| `Get(src, path...)` | the generic value, like `Parse` |
| `GetAs[T](src, path...)` | a scalar of type `T`: `int64`, `float64`, `string` or `bool` |
| `GetSlice[T](src, path...)` | a `[]T` from a pure array table, in index order |
| `Table.GetPath(keys...)` | the rich value from an already parsed `*Table` |

The typed variants follow Lua's number model: an integer is accepted as a
`float64`, and a `float64` with an integral value in range is accepted as an
`int64`, which is Lua's `math.tointeger`; nothing converts to or from a string.
`GetSlice` converts every element or reports `false`, so it never returns a
partially converted slice.

A lookup parses in lenient mode and accepts an optional `return` prefix, because
it is a query rather than a validation step; `Parse` and `ParseTable` remain the
way to check a whole input. When several values come from the same input, parse
once and walk:

```go
table, err := luatable.ParseTable(src)
if err != nil {
    log.Fatal(err)
}
host, _ := table.GetPath("servers", 1, "host")
port, _ := table.GetPath("servers", 1, "port")
```

## Generating Lua tables

`Marshal` performs the opposite operation: it turns Go data into a Lua table
literal. The output only contains literals and nested tables, so it can always
be read back by `Parse`.

```go
out, err := luatable.Marshal(map[string]any{
    "name": "demo",
    "tags": []any{"a", "b"},
})
if err != nil {
    log.Fatal(err)
}
fmt.Println(string(out))
// {name = "demo",tags = {"a","b"}}
```

Variants and options:

| Function | Output |
| --- | --- |
| `Marshal(v)` | compact: `{name = "demo"}` |
| `MarshalIndent(v, "  ")` | indented, one field per line |
| `MarshalModule(v)` | `return {name = "demo"}` + newline, the counterpart of `ParseModule` |
| `MarshalModuleIndent(v, "\t")` | indented module file |
| `Encoder.AppendMarshal(dst, v)` | appends the compact form to `dst`, so the caller can reuse one buffer |

`Encoder` carries the same options, with defaults chosen so that the zero value
is useful:

| Field | Default | Effect |
| --- | --- | --- |
| `Indent` | `""` | empty means compact single-line output |
| `MaxDepth` | `0` → `DefaultMaxDepth` | nesting limit, mirrors the parser |
| `TrailingComma` | `false` | write a `,` after the last field |
| `UnsortedKeys` | `false` | `false` sorts map keys, keeping output reproducible |
| `EmitSkipped` | `false` | write the raw text of a `Skipped` value back instead of rejecting it |

`Encoder.AppendMarshal` is the allocation-free path for callers that encode
repeatedly: reslicing one buffer to zero length before every call
(`buf, err = enc.AppendMarshal(buf[:0], v)`) reuses its capacity, and an
`Encoder` reuses the key lists it builds for maps with more than eight keys.
Once the buffer is large enough, encoding a value of the same shape allocates
nothing at all. A failed call returns `dst` unchanged, and the bytes it already
holds are never modified.

Values that cannot be represented — `struct`, `func`, `chan`, `NaN`, `±Inf`, a
`Skipped` value, or a `uint64` that does not fit into `int64` — are rejected with
an `*EncodeError` carrying the path of the offending value, for example
`.servers[0].port`. An infinity is rejected even though a literal such as `1e400`
happens to evaluate to one on every implementation tested: the manual leaves
float overflow to the implementation, so the encoder will not build a value out
of it. A `Skipped` value is rejected for the same reason, unless
`Encoder.EmitSkipped` asks for its text; see
[Values that are not literals](#values-that-are-not-literals).

Round-trip guarantees:

* `Parse(Marshal(v))` reproduces `v` for every value in the generic domain, with
  two documented exceptions: an empty slice encodes to `{}` and parses back as an
  empty `map[string]any`, and `math.MinInt64` (see below) comes back as the
  `float64` of the same value.
* Passing a `*Table` to `Marshal` preserves exact key types and, for non-array
  tables, insertion order. A pure array is written positionally, so its elements
  follow index order `1..n`.
* `map[string]any` keys are always written as string keys, so `[1]` and `["1"]`
  become indistinguishable — use a `*Table` when that matters.
* Keys that collide with a Lua reserved word are always quoted. The set is the
  union over Lua 5.1 – 5.5, so both `goto` (reserved since 5.2) and `global`
  (reserved since 5.5) are quoted, and the output compiles on every version.
* `math.MinInt64` is written in decimal, `-9223372036854775808`. Neither Lua nor
  this package has an integer literal for it, so the value comes back as a
  `float64` — exactly equal, but a different type. The hexadecimal form
  `0x8000000000000000` would preserve the type on Lua 5.3 and later, but it
  yields `+9223372036854775808` on Lua 5.1, Lua 5.2 and LuaJIT, which have no
  integer subtype at all.

## Module files

Lua data files are frequently written as a module:

```lua
return {
    debug = true,
    retries = 3,
}
```

Enable `Parser.AllowReturnPrefix`, or use the `ParseModule` helpers:

```go
table, err := luatable.ParseModule(luaSource)
```

## Supported syntax

```lua
{
    -- array part
    1, 2.5, .5, 1e10, 0xFF, 0x1p4,
    "escaped\nstring", 'single', [[long
string]], [=[level 2]=],

    -- hash part
    identifier = "value",
    ["quoted key"] = true,
    [42] = "numeric key",
    [1.5] = "float key",
    [false] = "boolean key",

    -- nested tables
    nested = { deeper = { 1, 2, 3 } },
}
```

## Unsupported syntax

Values must be literals, nested tables, unary minus or parenthesized literals.
Anything requiring evaluation — variable references, function calls, arithmetic,
concatenation (`math.huge`, `1 + 2`, `"a" .. "b"`) — is rejected with a
`*SyntaxError`. Set `Parser.Lenient` to record such a value as a `Skipped`
instead; see [Values that are not literals](#values-that-are-not-literals).

## Error handling

All errors are `*SyntaxError` values carrying `Offset`, `Line` and `Column`:

```go
_, err := luatable.Parse("{ a = }")

var syntaxErr *luatable.SyntaxError
if errors.As(err, &syntaxErr) {
    fmt.Printf("line %d, column %d: %s\n", syntaxErr.Line, syntaxErr.Column, syntaxErr.Msg)
}
// line 1, column 7: unexpected '}', expected a value
```

## Reusing a parser

A `Parser` can be reused to avoid repeated allocations, and `ParserPool` gives
a concurrency-safe pool. A `Parser` itself must not be shared between
goroutines.

```go
var pool luatable.ParserPool

p := pool.Get()
value, err := p.Parse(src)
pool.Put(p)
```

The package-level `Parse`, `ParseBytes`, `ParseTable`, `ParseModule`, … helpers
use an internal pool and are convenient for one-off parses.

## Limits

* Only table constructors are parsed; arbitrary Lua statements are not. A dump
  that wraps its table in a chunk (`local t = {...}` … `return t`) or in a
  `setmetatable` call is rejected even in lenient mode, because there is no
  table constructor to return.
* No metatables, functions, coroutines or variable evaluation.
* In the generic map representation a numeric key and a text key with the same
  spelling collide (`[1]` and `["1"]` both become `"1"`). Use `ParseTable` when
  that distinction matters. If such keys are also assigned more than once, the
  generic representation keeps the value of the last such field in the source,
  while the map derived from a `*Table` resolves the collapse by entry order.
* Columns are counted in bytes, not Unicode code points. A line break is LF,
  CR, CRLF or LFCR, as in Lua.
* Nesting depth is limited by `Parser.MaxDepth` (default `DefaultMaxDepth`, 300)
  and, symmetrically, by `Encoder.MaxDepth`.
* The encoder never writes long strings (`[[...]]`); strings are always emitted
  as escaped short strings, so the output stays safe on a single line.
* The parser is lenient by default: it accepts a reserved word as a bare table
  key (`{end = 1}`), which the reference implementation rejects. Set
  `Parser.StrictKeywords` to reject those keys instead. The encoder is always
  strict, because its output has to be valid Lua.
* `Parser.Lenient` recovers from a value it cannot decode; it is not a validation
  mode. Only the strict parser guarantees that every value is a literal.
* `Marshal` rejects a `Skipped` value, so a table parsed in lenient mode has to
  be cleaned up, or written with `Encoder.EmitSkipped`, before it can be
  encoded.

## Project layout

```
luatable/
├── .gitattributes          LF line endings in the repository and in checkouts
├── .gitignore
├── .github/workflows/
│   └── test.yml            CI: format, vet, race, coverage gate, fuzz smoke
├── go.mod                  module definition, no third-party dependencies
├── README.md
├── doc.go                  package documentation
├── errors.go               SyntaxError and error construction
├── util.go                 internal helpers (positionAt, truncate)
├── lexer.go                tokenizer: tokens, whitespace, comments, long brackets
├── number.go               Lua number literals (decimal, hex, hex float)
├── string.go               short and long string decoding
├── table.go                Table / Entry and generic-structure conversion
├── parser.go               recursive-descent parser and depth control
├── sink.go                 field sinks: the rich *Table or the generic value
├── lenient.go              lenient mode: Skipped values and expression skipping
├── selection.go            path lookup (Get, GetAs, GetSlice, Table.GetPath)
├── encode.go               Lua table generator (Marshal, Encoder, EncodeError)
├── pool.go                 ParserPool
├── convenience.go          package-level convenience functions
├── *_test.go               unit, example, fuzz and benchmark tests
├── *_lua_test.go           oracle tests run against installed Lua interpreters
└── testdata/
    ├── config.lua          configuration-table fixture
    ├── module.lua          "return { ... }" module fixture
    ├── comments.lua        comment-coverage fixture
    ├── dumper.lua          DataDumper fixture, used by the lenient-mode test
    └── kitchen_sink.lua    every supported construct, used by the fixture tests
```

The library is a **single package**, so every `package luatable` source file and
its tests live at the module root — the idiomatic layout for a single-package
library, and the same layout used by `fastjson`. Test fixtures live in
`testdata/`, which the `go` toolchain ignores.

## Development

```bash
gofmt -l .
go vet ./...
go test ./...
go test -race ./...
go test -cover ./...
go test -run=XXX -fuzz='^FuzzParse$' -fuzztime=30s .
go test -run=XXX -fuzz='^FuzzParseLenient$' -fuzztime=30s .
go test -run=XXX -fuzz='^FuzzMarshalString$' -fuzztime=30s .
go test -run=XXX -bench=. .
```

When a Lua interpreter is available on `PATH` (`lua`, `lua5.5`, … `luajit`), the
suite also feeds the encoder output to it and checks that it compiles and
evaluates to a table, and it compares the hexadecimal float conversion with the
interpreter's own `%a` rendering. The lenient-mode test additionally generates a
real dump with `testdata/dumper.lua` for every interpreter that can run it (Lua
5.1, a Lua 5.2 built with its compatibility options, or LuaJIT). Those checks live
in the `*_lua_test.go` files, and they are skipped when no interpreter is found
and in `-short` mode.
