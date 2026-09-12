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

func newEncodeError(path pathStack, format string, args ...any) *EncodeError {
	return &EncodeError{Msg: fmt.Sprintf(format, args...), Path: path.String()}
}

// pathKind describes one step of a value's position inside the encoded data.
type pathKind uint8

const (
	pathNone  pathKind = iota // no step: the root, or a key that cannot be written
	pathKey                   // a string key: ".name" or `["max-connections"]`
	pathIndex                 // an integer index or key: "[3]"
	pathFloat                 // a float key: "[1.5]"
	pathBool                  // a boolean key: "[true]"
)

// pathSeg is one step of the path to a value. The key is kept as it was seen,
// not as text, so that walking into a field costs no allocation: the path is
// rendered only when an error is reported.
//
// Every kind but pathKey stores its value in num: an index uses it directly, a
// float stores its bit pattern and a boolean stores 0 or 1. Keeping the struct
// at four words halves the memory a deeply nested value needs for its steps.
type pathSeg struct {
	kind pathKind
	text string
	num  int64
}

// pathStack is the path to the value being encoded. Each field pushes one step
// before descending and pops it right after, so the backing array is reused by
// every sibling and no step allocates: a whole encode needs no allocation once
// the stack has grown to the depth of the value.
type pathStack []pathSeg

// push appends one step.
func (p pathStack) push(seg pathSeg) pathStack {
	return append(p, seg)
}

// pop removes the last step. It is called after every push, including when the
// recursive call failed, so that the stack stays consistent.
func (p pathStack) pop() pathStack {
	return p[:len(p)-1]
}

// key pushes a string key, which is rendered as ".name" when it is a Lua
// identifier and as `["..."]` otherwise.
func (p pathStack) key(k string) pathStack {
	return p.push(pathSeg{kind: pathKey, text: k})
}

// index pushes an integer index or key, rendered as "[n]".
func (p pathStack) index(i int64) pathStack {
	return p.push(pathSeg{kind: pathIndex, num: i})
}

// entry pushes the key of a Table entry. A key the encoder cannot write is
// recorded as an empty step, which renders as nothing, so that every field
// pushes exactly one step and can pop unconditionally.
func (p pathStack) entry(key any) pathStack {
	switch k := key.(type) {
	case string:
		return p.key(k)
	case int64:
		return p.index(k)
	case float64:
		return p.push(pathSeg{kind: pathFloat, num: int64(math.Float64bits(k))})
	case bool:
		if k {
			return p.push(pathSeg{kind: pathBool, num: 1})
		}
		return p.push(pathSeg{kind: pathBool})
	default:
		return p.push(pathSeg{})
	}
}

// String renders the path the way EncodeError.Path has always looked, for
// example ".servers[0].port" or `["max-connections"]`. It is called only while
// reporting an error.
func (p pathStack) String() string {
	if len(p) == 0 {
		return ""
	}

	var b strings.Builder
	for _, seg := range p {
		switch seg.kind {
		case pathNone:
			// Nothing to render: a key that cannot be written.
		case pathKey:
			if isIdentifier(seg.text) {
				b.WriteByte('.')
				b.WriteString(seg.text)
			} else {
				b.WriteByte('[')
				b.WriteString(strconv.Quote(seg.text))
				b.WriteByte(']')
			}
		case pathIndex:
			b.WriteByte('[')
			b.WriteString(strconv.FormatInt(seg.num, 10))
			b.WriteByte(']')
		case pathFloat:
			b.WriteByte('[')
			b.WriteString(strconv.FormatFloat(math.Float64frombits(uint64(seg.num)), 'g', -1, 64))
			b.WriteByte(']')
		case pathBool:
			b.WriteByte('[')
			b.WriteString(strconv.FormatBool(seg.num != 0))
			b.WriteByte(']')
		}
	}
	return b.String()
}

// Encoder encodes Go values into Lua table literals.
//
// The zero value is ready to use: it writes compact, deterministic output that
// Parse reads back without error. An Encoder may be re-used — it keeps the
// scratch space that encoding needs, so re-use avoids allocating it again — but
// it must not be used from concurrent goroutines.
//
// The encoder accepts the same value domain that Parse produces (nil, bool,
// int64, float64, string, []any, map[string]any and *Table), plus a few
// convenience types such as int, uint, float32, []string and
// map[string]string. Any other value is rejected with an *EncodeError; the
// encoder never emits a literal that its own parser would reject, unless
// Encoder.EmitSkipped asks it to write the raw text of a Skipped value.
//
// Two values of that domain do not survive an exact round trip. NaN and the
// infinities are rejected outright: no literal is specified to denote an
// infinity, because the manual leaves float overflow to the implementation, and
// the encoder refuses to invent one out of an overflowing literal.
// math.MinInt64 is written in decimal, which Parse reads back as the float64 of
// the same value; see writeInt for why the hexadecimal form is not used.
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

	// EmitSkipped writes the raw source text of a Skipped value back instead
	// of rejecting it. It is meant for regenerating a file that was parsed
	// with Parser.Lenient, so that the fields the parser could not decode
	// survive a round trip.
	//
	// The output is then no longer checked, and it is no longer guaranteed to
	// be valid Lua or to be accepted by Parse: the text may be an expression
	// the parser rejects, and it may be code, such as a call to loadstring.
	// Use it only for text that is trusted.
	EmitSkipped bool

	// keyLists keeps one reusable key slice per nesting depth, so that an
	// encoder used more than once does not allocate a fresh list for every
	// map with more than eight keys. It is scratch space, not state.
	keyLists [][]string
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

// AppendMarshal appends the compact Lua table literal for v to dst and returns
// the extended slice. It is Marshal with a caller-provided buffer: reusing one
// slice across calls reuses its capacity instead of growing a fresh buffer, and
// the encoder reuses the key lists it builds for large maps, so a hot loop
// stops allocating entirely once both are large enough.
//
//	buf := make([]byte, 0, 4096)
//	for _, v := range values {
//		var err error
//		buf, err = enc.AppendMarshal(buf[:0], v)
//		if err != nil {
//			return err
//		}
//		use(buf)
//	}
//
// When v cannot be encoded, AppendMarshal returns dst unchanged together with
// the *EncodeError: the bytes dst already holds are never modified.
func (e *Encoder) AppendMarshal(dst []byte, v any) ([]byte, error) {
	return e.appendTableValue(dst, v, false)
}

func (e *Encoder) writeTableValue(v any, module bool) ([]byte, error) {
	return e.appendTableValue(nil, v, module)
}

// appendTableValue appends the encoded form of v to dst, prefixing "return "
// when module is set. Writing goes through a bytes.Buffer that starts on dst,
// so the caller's spare capacity is used before anything is allocated.
func (e *Encoder) appendTableValue(dst []byte, v any, module bool) ([]byte, error) {
	buf := bytes.NewBuffer(dst)
	if module {
		buf.WriteString("return ")
	}

	// The path stack starts in an array on the stack; it only reaches the
	// heap for a value nested deeper than the array is long.
	var pathBuf [16]pathSeg
	if err := e.encode(buf, v, 0, pathBuf[:0]); err != nil {
		// The bytes written so far sit past len(dst), so returning dst
		// unchanged shows the caller nothing of the partial output.
		return dst, err
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
func (e *Encoder) encode(buf *bytes.Buffer, v any, depth int, path pathStack) error {
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
	case Skipped:
		return e.writeSkipped(buf, x, path)
	default:
		return newEncodeError(path,
			"unsupported value type %T; convert it to nil, bool, int64, float64, string, []any, map[string]any or *Table", v)
	}

	return nil
}

// writeSkipped writes the raw text of a value the lenient parser skipped, when
// Encoder.EmitSkipped asks for it. Nothing is written otherwise: the text can
// be code or text that is not valid Lua, so it has to be an explicit choice.
// An empty text is still rejected, because it would leave a hole in the output
// such as "{a = }", which parses nowhere.
func (e *Encoder) writeSkipped(buf *bytes.Buffer, s Skipped, path pathStack) error {
	if !e.EmitSkipped {
		return newEncodeError(path,
			"value %s was skipped by the lenient parser and cannot be encoded; set Encoder.EmitSkipped to write its text back", s)
	}
	if s.Text == "" {
		return newEncodeError(path, "skipped value has no text to write back")
	}
	buf.WriteString(s.Text)
	return nil
}

// writeUnsigned writes an unsigned value, rejecting values that do not fit
// into an int64 so that the literal still parses back as an integer.
func (e *Encoder) writeUnsigned(buf *bytes.Buffer, u uint64, path pathStack) error {
	if u > math.MaxInt64 {
		return newEncodeError(path, "unsigned value %d does not fit into int64", u)
	}
	writeInt(buf, int64(u))
	return nil
}

// writeInt writes an integer literal in decimal, which every Lua version reads
// as the same number.
//
// math.MinInt64 is a documented exception to the exact round trip. Its decimal
// form "-9223372036854775808" is read as a unary minus applied to
// 9223372036854775808, a literal that overflows int64 and is therefore promoted
// to a float64, so Parse returns float64(-9223372036854775808) instead of the
// int64. Lua itself has the same quirk: no integer literal denotes
// math.mininteger there either, and math.type(-9223372036854775808) is "float".
//
// The hexadecimal form 0x8000000000000000 does read back as this int64, but it
// relies on the wraparound of hexadecimal integer literals introduced in Lua
// 5.3, and it yields +9223372036854775808 on Lua 5.1, Lua 5.2 and LuaJIT, which
// have no integer subtype at all. Getting the value right everywhere is worth
// more than preserving the type, so the decimal form is used.
func writeInt(buf *bytes.Buffer, n int64) {
	// AppendInt writes into a stack buffer, so no string is allocated for
	// the literal.
	var scratch [24]byte
	buf.Write(strconv.AppendInt(scratch[:0], n, 10))
}

// writeFloat writes a floating point literal in a form that Parse converts
// back to a float64 rather than to an int64.
func writeFloat(buf *bytes.Buffer, f float64, bitSize int, path pathStack) error {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return newEncodeError(path, "%v cannot be represented in a Lua table literal", f)
	}

	// AppendFloat writes into a stack buffer, so no string is allocated for
	// the literal; 32 bytes cover the longest "%g" rendering of a float64.
	var scratch [32]byte
	s := strconv.AppendFloat(scratch[:0], f, 'g', -1, bitSize)
	if !bytes.ContainsAny(s, ".eE") {
		// The literal looks like an integer; force the float form so that
		// parsing it back preserves the type.
		s = append(s, '.', '0')
	}
	buf.Write(s)
	return nil
}

// isVerbatimByte reports whether c can be copied into a short string without
// escaping: printable ASCII other than the quote and the backslash, which have
// a one-character escape of their own.
func isVerbatimByte(c byte) bool {
	return c >= 0x20 && c < 0x7F && c != '"' && c != '\\'
}

// writeString writes s as a double-quoted Lua short string. It is the exact
// inverse of decodeShortString: Parse can read the result back byte for byte.
//
// Verbatim text is copied in runs instead of byte by byte: a string without
// escapes costs one copy, and a run of non-ASCII text one copy per run rather
// than one per rune.
func writeString(buf *bytes.Buffer, s string) {
	buf.WriteByte('"')

	for i := 0; i < len(s); {
		// Find the longest run that can be written as it is. The run ends at
		// the first byte that has an escape or is not part of a well-formed
		// UTF-8 sequence, which the switch below then handles alone.
		j := i
		for j < len(s) {
			c := s[j]
			if isVerbatimByte(c) {
				j++
				continue
			}
			if c >= utf8.RuneSelf {
				if r, size := utf8.DecodeRuneInString(s[j:]); r != utf8.RuneError || size > 1 {
					j += size
					continue
				}
			}
			break
		}
		if n := j - i; n > 0 {
			// A short run is cheaper to write byte by byte than through a
			// call that has to reserve capacity; escaped text is mostly short
			// runs, and it must not pay for the long-run fast path.
			if n < 8 {
				for k := i; k < j; k++ {
					buf.WriteByte(s[k])
				}
			} else {
				buf.WriteString(s[i:j])
			}
			i = j
			continue
		}

		c := s[i]
		switch c {
		case '"':
			buf.WriteString(`\"`)
		case '\\':
			buf.WriteString(`\\`)
		case '\a':
			buf.WriteString(`\a`)
		case '\b':
			buf.WriteString(`\b`)
		case '\f':
			buf.WriteString(`\f`)
		case '\n':
			buf.WriteString(`\n`)
		case '\r':
			buf.WriteString(`\r`)
		case '\t':
			buf.WriteString(`\t`)
		case '\v':
			buf.WriteString(`\v`)
		default:
			// Control characters, DEL and bytes that are not part of a
			// well-formed UTF-8 sequence become decimal escapes.
			writeDecimalEscape(buf, c)
		}
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

func (e *Encoder) encodeArray(buf *bytes.Buffer, a []any, depth int, path pathStack) error {
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
		path = path.index(int64(i))
		err := e.encode(buf, v, depth+1, path)
		path = path.pop()
		if err != nil {
			return err
		}
	}
	e.closeTable(buf, depth)
	return nil
}

func (e *Encoder) encodeMap(buf *bytes.Buffer, m map[string]any, depth int, path pathStack) error {
	if len(m) == 0 {
		buf.WriteString("{}")
		return nil
	}
	if err := e.checkDepth(depth, path); err != nil {
		return err
	}

	// The common table is small; a stack array keeps its key list off the
	// heap. A larger map reuses the list the encoder keeps for this depth, so
	// encoding repeatedly with the same encoder does not allocate either.
	var scratch [8]string
	keys := scratch[:0]
	if len(m) > len(scratch) {
		keys = e.mapKeys(depth, m)
	} else {
		for k := range m {
			keys = append(keys, k)
		}
		if !e.UnsortedKeys {
			slices.Sort(keys)
		}
	}

	buf.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		e.indentln(buf, depth+1)
		writeStringKey(buf, k)
		buf.WriteString(" = ")
		path = path.key(k)
		err := e.encode(buf, m[k], depth+1, path)
		path = path.pop()
		if err != nil {
			return err
		}
	}
	e.closeTable(buf, depth)
	return nil
}

// mapKeys returns the sorted key list of m, reusing the list this encoder kept
// for the same nesting depth. The result is valid until the next call at that
// depth, which is all the caller needs: a map's keys are used while its own
// fields are encoded, and nested maps work at a deeper level.
func (e *Encoder) mapKeys(depth int, m map[string]any) []string {
	for len(e.keyLists) <= depth {
		e.keyLists = append(e.keyLists, nil)
	}

	keys := e.keyLists[depth][:0]
	if cap(keys) < len(m) {
		keys = make([]string, 0, len(m))
	}
	for k := range m {
		keys = append(keys, k)
	}
	e.keyLists[depth] = keys[:0]

	if !e.UnsortedKeys {
		slices.Sort(keys)
	}
	return keys
}

func (e *Encoder) encodeTable(buf *bytes.Buffer, t *Table, depth int, path pathStack) error {
	// The encoder only reads the entries, so the copy Entries makes for
	// callers is not needed here.
	entries := t.entriesNoCopy()
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
			path = path.index(int64(i + 1))
			err := e.encode(buf, v, depth+1, path)
			path = path.pop()
			if err != nil {
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
		path = path.entry(entry.Key)
		if err := writeEntryKey(buf, entry.Key, path); err != nil {
			return err
		}
		buf.WriteString(" = ")
		err := e.encode(buf, entry.Value, depth+1, path)
		path = path.pop()
		if err != nil {
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
func (e *Encoder) checkDepth(depth int, path pathStack) error {
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
func writeEntryKey(buf *bytes.Buffer, key any, path pathStack) error {
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
