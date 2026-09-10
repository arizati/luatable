package luatable

// handyPool backs the package level convenience functions. Those functions are
// convenient but slower than reusing a Parser, because each call has to obtain
// and release a parser from the pool.
var handyPool ParserPool

// Parse parses s as a single Lua table constructor and returns its generic
// representation.
//
// Reuse a Parser when parsing repeatedly for better performance.
func Parse(s string) (interface{}, error) {
	p := handyPool.Get()
	defer handyPool.Put(p)
	return p.Parse(s)
}

// ParseBytes parses b as a single Lua table constructor. See Parse.
func ParseBytes(b []byte) (interface{}, error) {
	p := handyPool.Get()
	defer handyPool.Put(p)
	return p.ParseBytes(b)
}

// MustParse is like Parse but panics when s cannot be parsed.
func MustParse(s string) interface{} {
	v, err := Parse(s)
	if err != nil {
		panic(err)
	}
	return v
}

// MustParseBytes is like ParseBytes but panics when b cannot be parsed.
func MustParseBytes(b []byte) interface{} {
	v, err := ParseBytes(b)
	if err != nil {
		panic(err)
	}
	return v
}

// ParseTable parses s and returns the rich *Table representation, preserving
// exact key types and insertion order.
func ParseTable(s string) (*Table, error) {
	p := handyPool.Get()
	defer handyPool.Put(p)
	return p.ParseTable(s)
}

// ParseTableBytes parses b and returns the rich *Table representation. See
// ParseTable.
func ParseTableBytes(b []byte) (*Table, error) {
	p := handyPool.Get()
	defer handyPool.Put(p)
	return p.ParseTableBytes(b)
}

// MustParseTable is like ParseTable but panics when s cannot be parsed.
func MustParseTable(s string) *Table {
	t, err := ParseTable(s)
	if err != nil {
		panic(err)
	}
	return t
}

// ParseModule parses s, accepting an optional leading "return" statement so
// that Lua module files of the form "return { ... }" can be parsed directly.
func ParseModule(s string) (*Table, error) {
	p := handyPool.Get()
	defer handyPool.Put(p)
	p.AllowReturnPrefix = true
	return p.ParseTable(s)
}

// ParseModuleBytes parses b, accepting an optional leading "return" statement.
// See ParseModule.
func ParseModuleBytes(b []byte) (*Table, error) {
	return ParseModule(string(b))
}

// MustParseModule is like ParseModule but panics when s cannot be parsed.
func MustParseModule(s string) *Table {
	t, err := ParseModule(s)
	if err != nil {
		panic(err)
	}
	return t
}
