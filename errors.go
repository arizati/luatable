package luatable

import "fmt"

// DefaultMaxDepth is the default maximum table nesting depth accepted by a
// Parser. Deeper input is rejected in order to protect against stack
// exhaustion caused by malicious or malformed input.
const DefaultMaxDepth = 300

// SyntaxError describes a problem detected while parsing a Lua table.
//
// It always carries the position of the offending construct: the zero-based
// byte offset together with the one-based line and column.
type SyntaxError struct {
	// Msg is the human readable reason without the position suffix.
	Msg string

	// Offset is the zero-based byte offset into the parsed input.
	Offset int

	// Line is the one-based line number of Offset.
	Line int

	// Column is the one-based column number of Offset, counted in bytes.
	Column int
}

// Error implements the error interface.
func (e *SyntaxError) Error() string {
	return fmt.Sprintf("luatable: %s at line %d, column %d (offset %d)", e.Msg, e.Line, e.Column, e.Offset)
}

// newSyntaxError builds a *SyntaxError for the given input and byte offset,
// deriving the line and column from src.
func newSyntaxError(src string, offset int, format string, args ...interface{}) *SyntaxError {
	line, column := positionAt(src, offset)
	return &SyntaxError{
		Msg:    fmt.Sprintf(format, args...),
		Offset: offset,
		Line:   line,
		Column: column,
	}
}
