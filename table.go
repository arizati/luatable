package luatable

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Entry is a single key/value pair of a Table.
//
// Key is one of string, int64, float64 or bool. Value is one of nil, bool,
// int64, float64, string, []any, map[string]any or *Table.
type Entry struct {
	Key   any
	Value any
}

// Table is the rich representation of a parsed Lua table. Unlike the generic
// representation returned by Parser.Parse, a Table preserves the exact key
// types and the original insertion order of the fields.
//
// Nested tables are stored as *Table values, so the full fidelity is available
// at every level. Use Array, Map, Interface or ToInterface when a plain Go
// structure is required.
type Table struct {
	entries []Entry
	index   map[any]int
}

// tableBuilder accumulates fields while a table constructor is parsed.
type tableBuilder struct {
	t *Table

	// next is the index assigned to the next positional (array) field.
	next int64
}

func newTableBuilder() *tableBuilder {
	return &tableBuilder{t: &Table{index: make(map[any]int)}}
}

// append adds a positional field, which receives the next array index.
func (tb *tableBuilder) append(value any) {
	tb.next++
	tb.t.set(int64(tb.next), value)
}

// set stores value under key, replacing any previous value for the same key
// while keeping the original insertion position.
func (tb *tableBuilder) set(key, value any) {
	tb.t.set(key, value)
}

func (tb *tableBuilder) build() *Table {
	return tb.t
}

func (t *Table) set(key, value any) {
	if i, ok := t.index[key]; ok {
		t.entries[i].Value = value
		return
	}
	t.index[key] = len(t.entries)
	t.entries = append(t.entries, Entry{Key: key, Value: value})
}

// Len returns the number of entries in t.
func (t *Table) Len() int {
	if t == nil {
		return 0
	}
	return len(t.entries)
}

// Entries returns a copy of the table entries in insertion order. Values may
// be nested *Table instances; call ToInterface to obtain a plain Go structure.
func (t *Table) Entries() []Entry {
	if t == nil {
		return nil
	}
	out := make([]Entry, len(t.entries))
	copy(out, t.entries)
	return out
}

// Get returns the value stored under key.
//
// Integer-valued float keys are normalized, so Get(1) and Get(1.0) refer to the
// same entry. The second result reports whether the key is present.
func (t *Table) Get(key any) (any, bool) {
	if t == nil || t.index == nil {
		return nil, false
	}
	normalized, ok := normalizeKey(key)
	if !ok {
		return nil, false
	}
	i, ok := t.index[normalized]
	if !ok {
		return nil, false
	}
	return t.entries[i].Value, true
}

// IsArray reports whether t is a pure array table, that is whether every key is
// a positive integer and the keys form exactly the sequence 1..n. An empty
// table is not considered an array.
func (t *Table) IsArray() bool {
	if t == nil || len(t.entries) == 0 {
		return false
	}
	n := int64(len(t.entries))
	for _, e := range t.entries {
		k, ok := e.Key.(int64)
		if !ok || k < 1 || k > n {
			return false
		}
	}
	// Keys are unique, so n distinct integers within [1, n] are exactly 1..n.
	return true
}

// Array returns the elements of t as a slice when t is a pure array table.
// It returns nil when t is not an array.
//
// Nested tables are converted to their generic representation.
func (t *Table) Array() []any {
	if !t.IsArray() {
		return nil
	}
	out := make([]any, len(t.entries))
	for _, e := range t.entries {
		out[e.Key.(int64)-1] = ToInterface(e.Value)
	}
	return out
}

// Map returns t as a generic map keyed by the string form of each key.
//
// Array indices are represented as decimal strings ("1", "2", ...). Nested
// tables are converted to their generic representation.
//
// Note that a numeric key and a text key with the same spelling collide, for
// example [1] and ["1"] both map to the key "1". Use ParseTable when that
// distinction matters.
func (t *Table) Map() map[string]any {
	m := make(map[string]any, t.Len())
	if t == nil {
		return m
	}
	for _, e := range t.entries {
		m[keyToString(e.Key)] = ToInterface(e.Value)
	}
	return m
}

// Interface returns the generic representation of t: a []any when t is
// a pure array table, and a map[string]any otherwise. An empty table
// yields an empty map.
func (t *Table) Interface() any {
	if t.IsArray() {
		return t.Array()
	}
	return t.Map()
}

// String returns a Lua-like representation of t. It is intended for debugging
// and is not guaranteed to round-trip.
func (t *Table) String() string {
	var b strings.Builder
	t.writeTo(&b)
	return b.String()
}

func (t *Table) writeTo(b *strings.Builder) {
	b.WriteByte('{')
	for i, e := range t.entries {
		if i > 0 {
			b.WriteString(", ")
		}
		switch k := e.Key.(type) {
		case string:
			if isIdentifier(k) {
				b.WriteString(k)
				b.WriteByte('=')
			} else {
				b.WriteByte('[')
				b.WriteString(strconv.Quote(k))
				b.WriteString("]=")
			}
		case int64:
			b.WriteByte('[')
			b.WriteString(strconv.FormatInt(k, 10))
			b.WriteString("]=")
		case float64:
			b.WriteByte('[')
			b.WriteString(strconv.FormatFloat(k, 'g', -1, 64))
			b.WriteString("]=")
		case bool:
			b.WriteByte('[')
			b.WriteString(strconv.FormatBool(k))
			b.WriteString("]=")
		default:
			b.WriteString("[?]=")
		}
		writeValue(b, e.Value)
	}
	b.WriteByte('}')
}

func writeValue(b *strings.Builder, v any) {
	switch x := v.(type) {
	case nil:
		b.WriteString("nil")
	case bool:
		b.WriteString(strconv.FormatBool(x))
	case int64:
		b.WriteString(strconv.FormatInt(x, 10))
	case float64:
		b.WriteString(strconv.FormatFloat(x, 'g', -1, 64))
	case string:
		b.WriteString(strconv.Quote(x))
	case *Table:
		x.writeTo(b)
	default:
		fmt.Fprint(b, x)
	}
}

// ToInterface recursively converts a parsed value into its generic
// representation. Values that are not *Table are returned unchanged.
func ToInterface(v any) any {
	if t, ok := v.(*Table); ok {
		return t.Interface()
	}
	return v
}

// Integer-valued float keys are canonicalized to int64 so that [1] and [1.0]
// refer to the same entry, matching Lua 5.3+ semantics.
const (
	minInt64Float = -9223372036854775808.0
	maxInt64Float = 9223372036854775808.0
)

// normalizeKey converts a key into its canonical, comparable form. The second
// result reports whether key is a valid table key.
func normalizeKey(key any) (any, bool) {
	switch k := key.(type) {
	case string:
		return k, true
	case bool:
		return k, true
	case int:
		return int64(k), true
	case int64:
		return k, true
	case float64:
		if math.IsNaN(k) || math.IsInf(k, 0) {
			return nil, false
		}
		if k == math.Trunc(k) && k >= minInt64Float && k < maxInt64Float {
			return int64(k), true
		}
		return k, true
	default:
		return nil, false
	}
}

// keyToString renders a table key the way it appears in the generic map
// representation.
func keyToString(key any) string {
	switch k := key.(type) {
	case string:
		return k
	case int64:
		return strconv.FormatInt(k, 10)
	case float64:
		return strconv.FormatFloat(k, 'g', -1, 64)
	case bool:
		return strconv.FormatBool(k)
	default:
		return fmt.Sprint(k)
	}
}
