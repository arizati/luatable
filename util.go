package luatable

// positionAt converts a zero-based byte offset within src into a one-based
// line and column. Columns are counted in bytes, not Unicode code points.
//
// A line break is one of the sequences Lua reads as one: LF, CR, CRLF or LFCR.
// A mixed pair counts once; two equal bytes are two line breaks, matching both
// the lexer and the reference implementation.
//
// It is only invoked while reporting errors, so the linear scan is not on the
// hot path and keeps the lexer free of bookkeeping state.
func positionAt(src string, offset int) (line, column int) {
	offset = min(max(offset, 0), len(src))

	line, column = 1, 1
	for i := 0; i < offset; i++ {
		c := src[i]
		if c != '\n' && c != '\r' {
			column++
			continue
		}

		line++
		column = 1
		// Consume the second half of a mixed pair, so that CRLF and LFCR
		// count as a single line break.
		if i+1 < offset && src[i+1] != c && (src[i+1] == '\n' || src[i+1] == '\r') {
			i++
		}
	}
	return line, column
}

// truncate returns s limited to at most n bytes, appending an ellipsis when the
// value has been shortened. It is used to keep error messages readable.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
