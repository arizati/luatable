# luatable

A pure Go reader and writer for Lua table constructors (Lua 5.1 – 5.5). No
third-party dependencies, no code generation, no reflection magic — just parse
a Lua table into a plain Go value, or generate one from Go data.

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
  `\ddd`, `\xHH`, `\z` and `\u{XXXX}`.
* Long strings (`[[...]]`, `[=[...]=]`, arbitrary levels).
* Decimal, hexadecimal and hexadecimal-float literals (Lua 5.2+), including
  exponent forms.
* Unary minus and parenthesized literal expressions.
* Line comments, block comments and long-bracket comments.
* `,` and `;` field separators, including trailing separators.
* Optional `return` prefix for Lua module files (`return { ... }`).
* Precise error positions: byte offset, line and column.
* Optional rich representation preserving exact key types and insertion order.
* Safe against hostile input: bounded nesting depth, no panics (fuzz-tested).
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

`Encoder` carries the same options, with defaults chosen so that the zero value
is useful:

| Field | Default | Effect |
| --- | --- | --- |
| `Indent` | `""` | empty means compact single-line output |
| `MaxDepth` | `0` → `DefaultMaxDepth` | nesting limit, mirrors the parser |
| `TrailingComma` | `false` | write a `,` after the last field |
| `UnsortedKeys` | `false` | `false` sorts map keys, keeping output reproducible |

Values that cannot be represented — `struct`, `func`, `chan`, `NaN`, `±Inf`, or
a `uint64` that does not fit into `int64` — are rejected with an `*EncodeError`
carrying the path of the offending value, for example `.servers[0].port`.

Round-trip guarantees:

* `Parse(Marshal(v))` reproduces `v` for every value in the generic domain, with
  one documented exception: an empty slice encodes to `{}` and parses back as an
  empty `map[string]any`.
* Passing a `*Table` to `Marshal` preserves exact key types and, for non-array
  tables, insertion order. A pure array is written positionally, so its elements
  follow index order `1..n`.
* `map[string]any` keys are always written as string keys, so `[1]` and `["1"]`
  become indistinguishable — use a `*Table` when that matters.
* Keys that collide with a Lua reserved word are always quoted. The set is the
  union over Lua 5.1 – 5.5, so both `goto` (reserved since 5.2) and `global`
  (reserved since 5.5) are quoted, and the output compiles on every version.
* `math.MinInt64` is written as `0x8000000000000000`, because its decimal form
  would be parsed back as a `float64`.

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
`*SyntaxError`.

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

* Only table constructors are parsed; arbitrary Lua statements are not.
* No metatables, functions, coroutines or variable evaluation.
* In the generic map representation a numeric key and a text key with the same
  spelling collide (`[1]` and `["1"]` both become `"1"`). Use `ParseTable` when
  that distinction matters.
* Columns are counted in bytes, not Unicode code points.
* Nesting depth is limited by `Parser.MaxDepth` (default `DefaultMaxDepth`, 300)
  and, symmetrically, by `Encoder.MaxDepth`.
* The encoder never writes long strings (`[[...]]`); strings are always emitted
  as escaped short strings, so the output stays safe on a single line.
* The parser is lenient by default: it accepts a reserved word as a bare table
  key (`{end = 1}`), which the reference implementation rejects. Set
  `Parser.StrictKeywords` to reject those keys instead. The encoder is always
  strict, because its output has to be valid Lua.

## Project layout

```
luatable/
├── .gitignore
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
├── encode.go               Lua table generator (Marshal, Encoder, EncodeError)
├── pool.go                 ParserPool
├── handy.go                package-level convenience functions
├── *_test.go               unit, example, fuzz and benchmark tests
└── testdata/
    ├── config.lua          configuration-table fixture
    ├── module.lua          "return { ... }" module fixture
    └── comments.lua        comment-coverage fixture
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
go test -run=XXX -fuzz='^FuzzMarshalString$' -fuzztime=30s .
go test -run=XXX -bench=. .
```

When a Lua interpreter is available on `PATH` (`lua`, `lua5.5`, … `luajit`), the
suite also feeds the encoder output to it and checks that it compiles and
evaluates to a table. That check is skipped when no interpreter is found and in
`-short` mode.
