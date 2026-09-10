package luatable

import (
	"bytes"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

// EncodeError describes a problem detected while encoding a Go value into a
// Lua table literal.
//
// Unlike SyntaxError, which points at a position inside the parsed input text,
// an EncodeError points at a path inside the encoded value.
type EncodeError struct {
	// Msg is the human readable reason, without the path.
	Msg string

	// Path is the location of the offending value inside the encoded data,
	// for example ".servers[0].port". It is empty for the root value.
	Path string
}

// Error implements the error interface.
func (e *EncodeError) Error() string {
	if e.Path == "" {
		return "luatable: encode error: " + e.Msg
	}
	return "luatable: encode error at " + e.Path + ": " + e.Msg
}

func newEncodeError(path, format string, args ...any) *EncodeError {
	return &EncodeError{Msg: fmt.Sprintf(format, args...), Path: path}
}

// Encoder encodes Go values into Lua table literals.
//
// The zero value is ready to use: it writes compact, deterministic output that
// Parse reads back without error. An Encoder may be re-used, but must not be
// used from concurrent goroutines.
//
// The encoder accepts the same value domain that Parse produces (nil, bool,
// int64, float64, string, []any, map[string]any and *Table), plus a few
// convenience types such as int, uint, float32, []string and
// map[string]string. Values outside that domain are rejected with an
// *EncodeError; the encoder never emits a literal that its own parser would
// reject.
type Encoder struct {
	// Indent is the indentation unit used for multi-line output. The zero
	// value (an empty string) produces compact single-line output.
	Indent string

	// MaxDepth limits the nesting depth of the encoded value. Zero means
	// DefaultMaxDepth.
	MaxDepth int

	// TrailingComma writes a ',' after the last field of every non-empty
	// table.
	TrailingComma bool

	// UnsortedKeys disables the sorting of map keys. By default (the zero
	// value) map keys are emitted in sorted order, which keeps the output
	// reproducible; when set, the keys are emitted in Go's randomised map
	// order instead.
	//
	// The field is the negation of "sort keys" so that the useful behaviour
	// is the zero value.
	UnsortedKeys bool
}

// Marshal encodes v as a compact Lua table literal.
//
// The output never contains anything but literal values, so it can always be
// read back with Parse. See the package documentation for the value mapping
// and for the cases in which Marshal and Parse are not exact inverses.
func Marshal(v any) ([]byte, error) {
	return MarshalIndent(v, "")
}

// MarshalIndent encodes v as a Lua table literal, indenting nested tables by
// indent. An empty indent produces compact output, like Marshal.
func MarshalIndent(v any, indent string) ([]byte, error) {
	e := Encoder{Indent: indent}
	return e.Marshal(v)
}

// MarshalModule encodes v as a Lua module file: a table literal preceded by
// "return" and followed by a newline. It is the counterpart of ParseModule.
func MarshalModule(v any) ([]byte, error) {
	return MarshalModuleIndent(v, "")
}

// MarshalModuleIndent is MarshalModule with indentation. See MarshalIndent.
func MarshalModuleIndent(v any, indent string) ([]byte, error) {
	e := Encoder{Indent: indent}
	return e.MarshalModule(v)
}

// Marshal encodes v as a compact Lua table literal. See Marshal.
func (e *Encoder) Marshal(v any) ([]byte, error) {
	return e.writeTableValue(v, false)
}

// MarshalModule encodes v as a Lua module file. See MarshalModule.
func (e *Encoder) MarshalModule(v any) ([]byte, error) {
	return e.writeTableValue(v, true)
}

func (e *Encoder) writeTableValue(v any, module bool) ([]byte, error) {
	var buf bytes.Buffer
	if module {
		buf.WriteString("return ")
	}
	if err := e.encode(&buf, v, 0, ""); err != nil {
		return nil, err
	}
	if module {
		buf.WriteByte('\n')
	}
	return buf.Bytes(), nil
}

func (e *Encoder) maxDepth() int {
	if e.MaxDepth > 0 {
		return e.MaxDepth
	}
	return DefaultMaxDepth
}

// encode writes v at the given nesting depth. The root value is at depth 0.
func (e *Encoder) encode(buf *bytes.Buffer, v any, depth int, path string) error {
	switch x := v.(type) {
	case nil:
		buf.WriteString("nil")
	case bool:
		buf.WriteString(strconv.FormatBool(x))
	case int64:
		writeInt(buf, x)
	case int:
		writeInt(buf, int64(x))
	case int8:
		writeInt(buf, int64(x))
	case int16:
		writeInt(buf, int64(x))
	case int32:
		writeInt(buf, int64(x))
	case uint8:
		writeInt(buf, int64(x))
	case uint16:
		writeInt(buf, int64(x))
	case uint32:
		writeInt(buf, int64(x))
	case uint:
		return e.writeUnsigned(buf, uint64(x), path)
	case uint64:
		return e.writeUnsigned(buf, x, path)
	case float64:
		return writeFloat(buf, x, 64, path)
	case float32:
		return writeFloat(buf, float64(x), 32, path)
	case string:
		writeString(buf, x)
	case []any:
		return e.encodeArray(buf, x, depth, path)
	case []string:
		return e.encodeArray(buf, toAnySlice(x), depth, path)
	case []int:
		return e.encodeArray(buf, toAnySlice(x), depth, path)
	case []int64:
		return e.encodeArray(buf, toAnySlice(x), depth, path)
	case []float32:
		return e.encodeArray(buf, toAnySlice(x), depth, path)
	case []float64:
		return e.encodeArray(buf, toAnySlice(x), depth, path)
	case []bool:
		return e.encodeArray(buf, toAnySlice(x), depth, path)
	case map[string]any:
		return e.encodeMap(buf, x, depth, path)
	case map[string]string:
		return e.encodeMap(buf, toAnyMap(x), depth, path)
	case map[string]int:
		return e.encodeMap(buf, toAnyMap(x), depth, path)
	case map[string]int64:
		return e.encodeMap(buf, toAnyMap(x), depth, path)
	case map[string]float64:
		return e.encodeMap(buf, toAnyMap(x), depth, path)
	case map[string]bool:
		return e.encodeMap(buf, toAnyMap(x), depth, path)
	case *Table:
		return e.encodeTable(buf, x, depth, path)
	case Table:
		return e.encodeTable(buf, &x, depth, path)
	default:
		return newEncodeError(path,
			"unsupported value type %T; convert it to nil, bool, int64, float64, string, []any, map[string]any or *Table", v)
	}

	return nil
}

// writeUnsigned writes an unsigned value, rejecting values that do not fit
// into an int64 so that the literal still parses back as an integer.
func (e *Encoder) writeUnsigned(buf *bytes.Buffer, u uint64, path string) error {
	if u > math.MaxInt64 {
		return newEncodeError(path, "unsigned value %d does not fit into int64", u)
	}
	writeInt(buf, int64(u))
	return nil
}

// writeInt writes an integer literal.
//
// Every value round trips through Parse except math.MinInt64, whose decimal
// form "-9223372036854775808" would be parsed as a unary minus applied to
// 9223372036854775808; that literal overflows int64 and is therefore promoted
// to a float64. Its hexadecimal form is read back as exactly this int64.
func writeInt(buf *bytes.Buffer, n int64) {
	if n == math.MinInt64 {
		buf.WriteString("0x8000000000000000")
		return
	}
	buf.WriteString(strconv.FormatInt(n, 10))
}

// writeFloat writes a floating point literal in a form that Parse converts
// back to a float64 rather than to an int64.
func writeFloat(buf *bytes.Buffer, f float64, bitSize int, path string) error {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return newEncodeError(path, "%v cannot be represented in a Lua table literal", f)
	}

	s := strconv.FormatFloat(f, 'g', -1, bitSize)
	if !strings.ContainsAny(s, ".eE") {
		// The literal looks like an integer; force the float form so that
		// parsing it back preserves the type.
		s += ".0"
	}
	buf.WriteString(s)
	return nil
}

// writeString writes s as a double-quoted Lua short string. It is the exact
// inverse of decodeShortString: Parse can read the result back byte for byte.
func writeString(buf *bytes.Buffer, s string) {
	buf.WriteByte('"')

	for i := 0; i < len(s); {
		c := s[i]
		switch c {
		case '"':
			buf.WriteString(`\"`)
			i++
			continue
		case '\\':
			buf.WriteString(`\\`)
			i++
			continue
		case '\a':
			buf.WriteString(`\a`)
			i++
			continue
		case '\b':
			buf.WriteString(`\b`)
			i++
			continue
		case '\f':
			buf.WriteString(`\f`)
			i++
			continue
		case '\n':
			buf.WriteString(`\n`)
			i++
			continue
		case '\r':
			buf.WriteString(`\r`)
			i++
			continue
		case '\t':
			buf.WriteString(`\t`)
			i++
			continue
		case '\v':
			buf.WriteString(`\v`)
			i++
			continue
		}

		if c >= 0x20 && c < 0x7F {
			// Printable ASCII, written verbatim.
			buf.WriteByte(c)
			i++
			continue
		}

		// Keep well-formed UTF-8 sequences as they are so that the output
		// stays readable; everything else (control characters and invalid
		// UTF-8 bytes) becomes a decimal escape.
		if c >= utf8.RuneSelf {
			if r, size := utf8.DecodeRuneInString(s[i:]); r != utf8.RuneError || size > 1 {
				buf.WriteString(s[i : i+size])
				i += size
				continue
			}
		}

		writeDecimalEscape(buf, c)
		i++
	}

	buf.WriteByte('"')
}

// writeDecimalEscape writes \ddd with exactly three digits. Lua reads up to
// three digits greedily, so a two-digit escape followed by a digit character
// would be misread (for example "\12" + "3" would decode to '\n' + "3").
func writeDecimalEscape(buf *bytes.Buffer, c byte) {
	buf.WriteByte('\\')
	buf.WriteByte('0' + c/100)
	buf.WriteByte('0' + (c/10)%10)
	buf.WriteByte('0' + c%10)
}

func (e *Encoder) encodeArray(buf *bytes.Buffer, a []any, depth int, path string) error {
	if len(a) == 0 {
		buf.WriteString("{}")
		return nil
	}
	if err := e.checkDepth(depth, path); err != nil {
		return err
	}

	buf.WriteByte('{')
	for i, v := range a {
		if i > 0 {
			buf.WriteByte(',')
		}
		e.indentln(buf, depth+1)
		if err := e.encode(buf, v, depth+1, indexPath(path, i)); err != nil {
			return err
		}
	}
	e.closeTable(buf, depth)
	return nil
}

func (e *Encoder) encodeMap(buf *bytes.Buffer, m map[string]any, depth int, path string) error {
	if len(m) == 0 {
		buf.WriteString("{}")
		return nil
	}
	if err := e.checkDepth(depth, path); err != nil {
		return err
	}

	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	if !e.UnsortedKeys {
		slices.Sort(keys)
	}

	buf.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		e.indentln(buf, depth+1)
		writeStringKey(buf, k)
		buf.WriteString(" = ")
		if err := e.encode(buf, m[k], depth+1, keyPath(path, k)); err != nil {
			return err
		}
	}
	e.closeTable(buf, depth)
	return nil
}

func (e *Encoder) encodeTable(buf *bytes.Buffer, t *Table, depth int, path string) error {
	entries := t.Entries()
	if len(entries) == 0 {
		buf.WriteString("{}")
		return nil
	}
	if err := e.checkDepth(depth, path); err != nil {
		return err
	}

	if t.IsArray() {
		// A pure array is written positionally, so the elements must follow
		// index order 1..n rather than insertion order.
		values := make([]any, len(entries))
		for _, entry := range entries {
			values[entry.Key.(int64)-1] = entry.Value
		}
		buf.WriteByte('{')
		for i, v := range values {
			if i > 0 {
				buf.WriteByte(',')
			}
			e.indentln(buf, depth+1)
			if err := e.encode(buf, v, depth+1, indexPath(path, i+1)); err != nil {
				return err
			}
		}
		e.closeTable(buf, depth)
		return nil
	}

	buf.WriteByte('{')
	for i, entry := range entries {
		if i > 0 {
			buf.WriteByte(',')
		}
		e.indentln(buf, depth+1)
		valuePath := entryPath(path, entry.Key)
		if err := writeEntryKey(buf, entry.Key, valuePath); err != nil {
			return err
		}
		buf.WriteString(" = ")
		if err := e.encode(buf, entry.Value, depth+1, valuePath); err != nil {
			return err
		}
	}
	e.closeTable(buf, depth)
	return nil
}

// closeTable writes the optional trailing comma, the closing newline and the
// closing brace of a table whose fields have already been written.
func (e *Encoder) closeTable(buf *bytes.Buffer, depth int) {
	if e.TrailingComma {
		buf.WriteByte(',')
	}
	e.indentln(buf, depth)
	buf.WriteByte('}')
}

// indentln writes a newline followed by depth indentation units. It writes
// nothing in compact mode.
func (e *Encoder) indentln(buf *bytes.Buffer, depth int) {
	if e.Indent == "" {
		return
	}
	buf.WriteByte('\n')
	for range depth {
		buf.WriteString(e.Indent)
	}
}

// checkDepth reports an error when entering one more container would exceed
// the configured maximum depth.
func (e *Encoder) checkDepth(depth int, path string) error {
	if maxDepth := e.maxDepth(); depth+1 > maxDepth {
		return newEncodeError(path, "nesting depth exceeds the maximum of %d", maxDepth)
	}
	return nil
}

// writeStringKey writes a string key in its shortest valid form: "name" when
// it is a Lua identifier, and '["..."]' otherwise.
func writeStringKey(buf *bytes.Buffer, key string) {
	if isIdentifier(key) {
		buf.WriteString(key)
		return
	}
	buf.WriteByte('[')
	writeString(buf, key)
	buf.WriteByte(']')
}

// writeEntryKey writes a non-string table key, always in bracket form.
func writeEntryKey(buf *bytes.Buffer, key any, path string) error {
	switch k := key.(type) {
	case string:
		writeStringKey(buf, k)
		return nil
	case int64:
		buf.WriteByte('[')
		writeInt(buf, k)
		buf.WriteByte(']')
		return nil
	case float64:
		buf.WriteByte('[')
		if err := writeFloat(buf, k, 64, path); err != nil {
			return err
		}
		buf.WriteByte(']')
		return nil
	case bool:
		buf.WriteByte('[')
		buf.WriteString(strconv.FormatBool(k))
		buf.WriteByte(']')
		return nil
	default:
		return newEncodeError(path, "unsupported table key type %T", key)
	}
}

func indexPath(path string, i int) string {
	return path + "[" + strconv.Itoa(i) + "]"
}

func keyPath(path, key string) string {
	if isIdentifier(key) {
		return path + "." + key
	}
	return path + "[" + strconv.Quote(key) + "]"
}

func entryPath(path string, key any) string {
	switch k := key.(type) {
	case string:
		return keyPath(path, k)
	case int64:
		return path + "[" + strconv.FormatInt(k, 10) + "]"
	case float64:
		return path + "[" + strconv.FormatFloat(k, 'g', -1, 64) + "]"
	case bool:
		return path + "[" + strconv.FormatBool(k) + "]"
	default:
		return path
	}
}

// toAnySlice converts a slice of a supported element type into []any. It keeps
// the convenience type switch in encode small and free of repetition.
func toAnySlice[T any](in []T) []any {
	out := make([]any, len(in))
	for i, v := range in {
		out[i] = v
	}
	return out
}

// toAnyMap converts a map with string keys into map[string]any.
func toAnyMap[V any](in map[string]V) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
