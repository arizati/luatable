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
// int64, float64, string, []any, map[string]any, *Table, or Skipped when the
// parser ran in lenient mode.
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

// set stores value under key, replacing any previous value for the same key
// while keeping the original insertion position. The parser uses it for both
// kinds of field of a rich table: a keyed field passes its canonicalized key
// and a positional field passes the array index the constructor gave it.
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

// entriesNoCopy returns the live entry slice without copying. It is for
// internal callers that only read the entries, such as the encoder; Entries is
// the exported accessor, which copies so that a caller cannot see later
// changes to the table through the slice it was handed.
func (t *Table) entriesNoCopy() []Entry {
	if t == nil {
		return nil
	}
	return t.entries
}

// Get returns the value stored under key.
//
// The key may be a string, a bool, a float64 or any Go integer type: integer
// keys of every width are normalized to int64, and integer-valued float keys
// likewise, so Get(1), Get(int32(1)) and Get(1.0) all refer to the same entry.
// NaN and infinite keys always report absence. The second result reports
// whether the key is present.
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
	if t == nil {
		return map[string]any{}
	}
	m := make(map[string]any, len(t.entries))
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
	case Skipped:
		b.WriteString(x.String())
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

// normalizeKey converts a key into its canonical, comparable form: keys of
// every Go integer type are narrowed to int64, and integer-valued float64
// keys are canonicalized to int64 as well. The second result reports whether
// key is a valid table key.
func normalizeKey(key any) (any, bool) {
	switch k := key.(type) {
	case string:
		return k, true
	case bool:
		return k, true
	case int:
		return int64(k), true
	case int8:
		return int64(k), true
	case int16:
		return int64(k), true
	case int32:
		return int64(k), true
	case int64:
		return k, true
	case uint:
		if uint64(k) > math.MaxInt64 {
			return nil, false
		}
		return int64(k), true
	case uint8:
		return int64(k), true
	case uint16:
		return int64(k), true
	case uint32:
		return int64(k), true
	case uint64:
		if k > math.MaxInt64 {
			return nil, false
		}
		return int64(k), true
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
