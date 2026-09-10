package luatable

// positionAt converts a zero-based byte offset within src into a one-based
// line and column. Columns are counted in bytes, not Unicode code points.
//
// It is only invoked while reporting errors, so the linear scan is not on the
// hot path and keeps the lexer free of bookkeeping state.
func positionAt(src string, offset int) (line, column int) {
	if offset < 0 {
		offset = 0
	}
	if offset > len(src) {
		offset = len(src)
	}

	line, column = 1, 1
	for i := 0; i < offset; i++ {
		if src[i] == '\n' {
			line++
			column = 1
		} else {
			column++
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
