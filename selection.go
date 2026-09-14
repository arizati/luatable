package luatable

// Get returns the value at path inside the table constructor in src.
//
// Each element of path is a Lua table key: a string, a bool, a float64 or any
// Go integer type, normalized the way Table.Get normalizes keys, so that 1,
// int64(1) and 1.0 all refer to the same entry. Positional (array) keys start
// at 1, as they do in Lua: Get(`{ "a", "b" }`, 1) is "a".
//
// The result uses the generic representation of Parse, so a nested table
// becomes a map[string]any or a []any. Get is a query rather than a validation
// step: it parses in lenient mode and accepts an optional "return" prefix, so
// a value that cannot be decoded elsewhere in the input becomes a Skipped and
// does not stop the lookup. Use Parse or ParseTable when the whole input has to
// be checked.
//
// The second result reports whether the path was found; a field whose value is
// nil is present, so it reports true together with a nil value. The error is
// always a *SyntaxError. A missing path is not an error.
//
// Parse once with ParseTable and walk with Table.GetPath when several values
// are needed from the same input, or when the input nests deeper than
// DefaultMaxDepth: Get applies that limit and has no way to raise it.
func Get(src string, path ...any) (any, bool, error) {
	value, ok, err := getValue(src, path...)
	if err != nil || !ok {
		return nil, false, err
	}
	return ToInterface(value), true, nil
}

// Scalar is the set of scalar Lua values a lookup produces. It is the type set
// of As, GetAs and GetSlice, which have to be told the expected type at the
// call site, as in GetAs[int64](src, "port").
//
// The constraint lists exact types on purpose. With an approximation such as
// ~int64 a caller's named type would satisfy it, but the conversion can only
// see the dynamic type of the parsed value, so the conversion would have to
// fall back to reflection; with exact types the type set stays closed and every
// case is known at compile time.
type Scalar interface {
	int64 | float64 | string | bool
}

// As converts v into the scalar type T, reporting whether the conversion is
// possible. It is GetAs at the value level: a lookup hands a value out as an
// any, and As gives it the type the caller expects.
//
// It takes the two results of a lookup as its two arguments, so the pair passes
// through directly, without naming them:
//
//	port, ok := luatable.As[int64](table.Get("port"))
//
//	value, ok := table.GetPath("servers", 1, "port")
//	port, ok = luatable.As[int64](value, ok)
//
// The conversion follows Lua's number model, as GetAs does: an int64 is
// accepted as a float64, and a float64 is accepted as an int64 when it has an
// integral value in range, which is Lua's math.tointeger; a string and a bool
// convert only to their own type, and nil, a nested table and a Skipped value
// never convert.
//
// A value that was built by hand converts like a parsed one: an integer of any
// Go width is accepted in either number direction, as long as it fits into an
// int64, which is the range in which this package represents an integer. That
// is what makes As usable on a map[string]any the caller filled itself, where
// an integer literal has the Go type int.
//
// The second result is false when the lookup reported no value, and when the
// value cannot be converted.
func As[T Scalar](v any, ok bool) (T, bool) {
	var zero T
	if !ok {
		return zero, false
	}
	return convertScalar[T](v)
}

// AsSlice converts a slice of generic values into a []T, the way GetSlice
// converts the elements of a pure array table. It is GetSlice at the value
// level: it takes a []any that was already read, and the presence flag that
// came with it, as its two arguments:
//
//	raw, ok := config["ports"].([]any)
//	ports, ok := luatable.AsSlice[int64](raw, ok)
//
// The conversion is all or nothing, as in GetSlice: the second result is false
// when the flag is false, when elements is nil, which is how Table.Array
// reports a value that is not a pure array, and when any element cannot be
// converted. The slice is never returned partially converted. An empty non-nil
// slice is an array with no elements, so it converts to an empty []T.
//
// GetSlice converts the entries of a table in place, so it does not build the
// intermediate []any that this function needs.
func AsSlice[T Scalar](elements []any, ok bool) ([]T, bool) {
	if !ok || elements == nil {
		return nil, false
	}

	converted := make([]T, len(elements))
	for i, element := range elements {
		scalar, ok := convertScalar[T](element)
		if !ok {
			return nil, false
		}
		converted[i] = scalar
	}
	return converted, true
}

// GetAs returns the scalar at path as a T. It is Get with a type: the second
// result is false when the path is missing or when the value is not a scalar of
// that type.
//
// The conversion follows Lua's number model. An int64 is accepted as a float64,
// and a float64 is accepted as an int64 when it has an integral value in range,
// which is Lua's math.tointeger; strings, booleans, nil, tables and Skipped
// values never convert. The error is always a *SyntaxError, as in Get.
//
// GetAs parses src on every call; use As to convert a value that a *Table
// handed out after one parse.
func GetAs[T Scalar](src string, path ...any) (T, bool, error) {
	value, found, err := getValue(src, path...)
	if err != nil {
		var zero T
		return zero, false, err
	}
	// As applies the conversion GetAs applies to the value it found, so the
	// two entry points cannot drift apart.
	scalar, ok := As[T](value, found)
	return scalar, ok, nil
}

// GetSlice returns the elements of a pure array table as a []T, converting
// every element the way GetAs converts a single value. The elements are
// returned in index order, whatever order the fields were written in.
//
// The second result is false when the path is missing, when the value is not a
// pure array (keys exactly 1..n), or when any element cannot be converted: the
// slice is never returned partially converted. An empty table is not an array,
// so it reports false as well.
//
// GetSlice parses src on every call; use AsSlice to convert an array that a
// parsed value already holds.
func GetSlice[T Scalar](src string, path ...any) ([]T, bool, error) {
	value, ok, err := getValue(src, path...)
	if err != nil || !ok {
		return nil, false, err
	}

	table, ok := value.(*Table)
	if !ok || !table.IsArray() {
		return nil, false, nil
	}

	elements := make([]T, len(table.entries))
	for _, entry := range table.entries {
		// IsArray guarantees positive integer keys within 1..n, so the
		// assertion cannot fail, and every index is written exactly once.
		index := entry.Key.(int64)
		scalar, ok := convertScalar[T](entry.Value)
		if !ok {
			return nil, false, nil
		}
		elements[index-1] = scalar
	}
	return elements, true, nil
}

// getValue parses src and returns the value at path in the rich representation.
// It is the shared lookup behind Get, GetAs and GetSlice: a query parses in
// lenient mode and accepts an optional "return" prefix, because a value that
// cannot be decoded elsewhere in the input must not stop the lookup.
func getValue(src string, path ...any) (any, bool, error) {
	p := defaultPool.Get()
	defer defaultPool.Put(p)
	p.Lenient = true
	p.AllowReturnPrefix = true

	table, err := p.ParseTable(src)
	if err != nil {
		return nil, false, err
	}
	value, ok := table.GetPath(path...)
	if !ok {
		return nil, false, nil
	}
	return value, true, nil
}

// convertScalar converts a parsed value into the scalar type T, reporting
// whether the conversion is possible. The switch on any(&zero) is what keeps a
// single generic function honest: the type set of Scalar is closed, so every
// pointer case is known at compile time and no reflection is involved.
//
// The two number cases accept the same input, so that a value converts the same
// way whichever direction it is asked for: a float64 is itself, and the integer
// a value denotes, if it denotes one, converts to either number type. GetAs and
// GetSlice only ever see int64 and float64, but As takes a value from anywhere,
// and an integer there is of whatever width the caller's code gave it.
func convertScalar[T Scalar](v any) (T, bool) {
	var zero T

	switch p := any(&zero).(type) {
	case *int64:
		// normalizeKey applies the rule a table lookup applies, which is the
		// rule for this direction: an integer-valued float becomes an int64,
		// an integer of any Go width is narrowed to one, and a float with a
		// fractional part, NaN, an infinity and an out-of-range value do not
		// convert.
		if normalized, ok := normalizeKey(v); ok {
			if n, isInt := normalized.(int64); isInt {
				*p = n
				return zero, true
			}
		}
	case *float64:
		// The two types a parsed value can have convert directly. Any other
		// integer width, which only a value built by hand carries, falls back
		// to the general rule, so that both directions accept the same values
		// without putting a type switch in the way of the common ones.
		switch n := v.(type) {
		case float64:
			*p = n
			return zero, true
		case int64:
			*p = float64(n)
			return zero, true
		}
		if n, ok := integerValue(v); ok {
			*p = float64(n)
			return zero, true
		}
	case *string:
		if s, ok := v.(string); ok {
			*p = s
			return zero, true
		}
	case *bool:
		if b, ok := v.(bool); ok {
			*p = b
			return zero, true
		}
	}
	return zero, false
}

// integerValue returns the integer that v denotes, applying the rule a table
// lookup applies to a key: an integer of any Go width is narrowed to int64, and
// an integer-valued float64 within the int64 range is canonicalized the same
// way. It reports false for a float64 with a fractional part, for NaN and the
// infinities, which are not integers, and for an integer too large for an
// int64, which is how this package reports that it cannot represent a value as
// a Lua integer.
func integerValue(v any) (int64, bool) {
	normalized, ok := normalizeKey(v)
	if !ok {
		return 0, false
	}
	n, ok := normalized.(int64)
	return n, ok
}

// GetPath returns the value at path inside t, walking one key per element.
//
// The keys are the same as for Table.Get, and positional keys start at 1. The
// value of a nested table is returned as a *Table, so a walk can continue with
// another GetPath call; use ToInterface to obtain the generic representation.
// An empty path returns t itself.
//
// The second result reports whether the path was found. It is false when an
// element is missing, when the walk reaches a value that is not a table, or
// when an element is not a valid table key.
func (t *Table) GetPath(keys ...any) (any, bool) {
	if t == nil {
		return nil, false
	}

	var current any = t
	for _, key := range keys {
		table, ok := current.(*Table)
		if !ok {
			return nil, false
		}
		value, ok := table.Get(key)
		if !ok {
			return nil, false
		}
		current = value
	}
	return current, true
}
