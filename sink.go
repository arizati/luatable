package luatable

import (
	"math"
	"strconv"
)

// genericBuilder builds the generic representation of one table constructor,
// the []any or map[string]any that Parse returns, without going through a
// *Table. The parser holds one by value and resets it for each constructor it
// opens, so building a table allocates nothing beyond the result itself.
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

// reset prepares g to collect a new table constructor.
func (g *genericBuilder) reset() {
	*g = genericBuilder{
		allInts: true,
		lo:      math.MaxInt64,
		hi:      math.MinInt64,
	}
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
