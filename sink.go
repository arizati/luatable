package luatable

import (
	"math"
	"strconv"
)

// tableSink receives the fields of one table constructor while it is parsed. It
// is the seam that lets a single recursive descent feed two representations:
// the rich *Table that ParseTable returns, or the generic []any /
// map[string]any that Parse returns.
//
// Parsing straight into the generic representation avoids building a *Table
// that Parse would convert and throw away, so the index map, the ordered entry
// slice and the table itself are never allocated for it.
type tableSink interface {
	// positional stores a positional field under the array index the
	// constructor gives it (1, 2, ...). The parser numbers the fields, so a
	// sink only has to store them.
	positional(index int64, value any)

	// keyed stores a field under a key that normalizeKey has already validated
	// and canonicalized.
	keyed(key, value any)

	// result returns the finished table: a *Table for the rich sink, and the
	// generic representation for the other.
	result() any
}

// newTableSink returns a sink that builds the rich representation.
func newTableSink() tableSink {
	return newTableBuilder()
}

// newGenericSink returns a sink that builds the generic representation.
func newGenericSink() tableSink {
	return &genericBuilder{
		allInts: true,
		lo:      math.MaxInt64,
		hi:      math.MinInt64,
	}
}

// genericBuilder builds the generic representation of a table constructor
// directly, without going through a *Table.
//
// While only positional fields have been seen, they are appended to values:
// their keys are 1..len(values) by construction, so nothing else has to be
// recorded. The first keyed field materializes the map, moving the positional
// prefix into it, and every later field is stored there.
//
// Whether the result is an array is a table-wide decision that cannot be made
// before the last field is parsed, so it is deferred to result, which decides
// from the key set alone. That is what keeps the common shapes cheap: a table
// with no keyed field never allocates anything but its own values.
type genericBuilder struct {
	// values holds the positional fields until the map is materialized. For a
	// table with no keyed field it is the result itself, so it must not be
	// reused as scratch space.
	values []any

	// m is the generic map, created by the first keyed field. Its keys are the
	// string forms of the table keys, as in Table.Map.
	m map[string]any

	// allInts reports whether every key stored in m came from an int64 key.
	// Only then can the table still be an array: a single string, boolean or
	// float key rules that out for good.
	allInts bool

	// lo and hi are the smallest and largest int64 keys stored in m, starting
	// from the extremes of the range so that min and max can fold into them.
	// They are only read while allInts holds, which guarantees that both were
	// assigned: the map exists, so a field was stored, and every key that went
	// into it was an int64.
	lo, hi int64
}

func (g *genericBuilder) positional(index int64, value any) {
	if g.m == nil {
		g.values = append(g.values, value)
		return
	}
	g.storeIndex(index, value)
}

func (g *genericBuilder) keyed(key, value any) {
	if g.m == nil {
		g.materialize()
	}
	if index, ok := key.(int64); ok {
		g.storeIndex(index, value)
		return
	}
	g.allInts = false
	g.m[keyToString(key)] = value
}

// materialize moves the positional prefix into the map that the first keyed
// field requires, under the keys 1..len(values). The prefix goes through
// storeIndex like every other field, so the array verdict sees it as well.
func (g *genericBuilder) materialize() {
	g.m = make(map[string]any, len(g.values)+1)
	for i, value := range g.values {
		g.storeIndex(int64(i+1), value)
	}
	g.values = nil
}

// storeIndex writes a field under an integer key, which is what a positional
// field and an integer table key both reduce to once the table is a map.
func (g *genericBuilder) storeIndex(index int64, value any) {
	g.lo, g.hi = min(g.lo, index), max(g.hi, index)
	g.m[strconv.FormatInt(index, 10)] = value
}

func (g *genericBuilder) result() any {
	if g.m == nil {
		// A table without keyed fields has exactly the keys 1..len(values), so
		// it is an array. An empty table is not one, and parses to an empty
		// map, matching Table.Interface.
		if len(g.values) == 0 {
			return map[string]any{}
		}
		return g.values
	}

	// The table is an array when its keys are exactly 1..n, where n is the
	// number of entries. The keys are distinct integers, so they all lie in
	// [1, n] exactly when the smallest is at least 1 and the largest is at
	// most n; an n-element subset of [1, n] is [1, n] itself, and every index
	// is then written exactly once.
	if n := len(g.m); g.allInts && g.lo >= 1 && g.hi <= int64(n) {
		out := make([]any, n)
		for key, value := range g.m {
			// allInts means keyToString produced this key from an int64, so it
			// is a canonical decimal number in [1, n] and cannot fail.
			index, _ := strconv.ParseInt(key, 10, 64)
			out[index-1] = value
		}
		return out
	}
	return g.m
}
