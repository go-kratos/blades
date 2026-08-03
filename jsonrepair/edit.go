package jsonrepair

import (
	"errors"
	"fmt"
)

var (
	// ErrLimitExceeded reports that a configured resource limit was reached.
	ErrLimitExceeded = errors.New("json repair limit exceeded")
	// ErrInvalidOutput reports an internal repair that failed final validation.
	ErrInvalidOutput = errors.New("json repair produced invalid output")
)

// Result contains validated JSON and the edits used to produce it.
type Result struct {
	JSON  []byte
	Edits []Edit
}

// Changed reports whether repair changed the input.
func (result Result) Changed() bool {
	return len(result.Edits) > 0
}

// Edit describes one source-relative change. Start and End are byte offsets
// into the original input.
type Edit struct {
	Kind        EditKind
	Start       int
	End         int
	Replacement string
}

// EditKind identifies a repair transformation.
type EditKind string

const (
	// EditRewriteDocument replaces malformed input with a recovered JSON
	// serialization.
	EditRewriteDocument EditKind = "rewrite_document"
)

// Error reports a repair category and a zero-based byte offset. It never
// includes source content.
type Error struct {
	Err    error
	Offset int
}

// Error returns a source-safe diagnostic.
func (err *Error) Error() string {
	return fmt.Sprintf("%v at byte %d", err.Err, err.Offset)
}

// Unwrap exposes the repair category to errors.Is.
func (err *Error) Unwrap() error {
	return err.Err
}

func newError(kind error, offset int) *Error {
	return &Error{Err: kind, Offset: offset}
}

func applyEdits(input []byte, edits []Edit, maxOutputBytes int) ([]byte, error) {
	outputBytes := len(input)
	previousEnd := 0
	for _, edit := range edits {
		if edit.Start < previousEnd || edit.Start < 0 || edit.End < edit.Start || edit.End > len(input) {
			return nil, newError(ErrInvalidOutput, edit.Start)
		}
		outputBytes += len(edit.Replacement) - (edit.End - edit.Start)
		if outputBytes > maxOutputBytes {
			return nil, newError(ErrLimitExceeded, edit.Start)
		}
		previousEnd = edit.End
	}

	output := make([]byte, 0, outputBytes)
	cursor := 0
	for _, edit := range edits {
		output = append(output, input[cursor:edit.Start]...)
		output = append(output, edit.Replacement...)
		cursor = edit.End
	}
	output = append(output, input[cursor:]...)
	return output, nil
}
