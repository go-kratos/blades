package jsonrepair

import (
	"errors"
	"fmt"
)

var (
	// ErrUnrepairable reports syntax that cannot be fixed by insertion alone.
	ErrUnrepairable = errors.New("json is not conservatively repairable")
	// ErrAmbiguous reports input with more than one plausible local repair.
	ErrAmbiguous = errors.New("json repair is ambiguous")
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

// Changed reports whether repair inserted any bytes.
func (result Result) Changed() bool {
	return len(result.Edits) > 0
}

// Edit describes one source-relative change. Conservative repairs are always
// insertions, so Start and End are equal byte offsets into the original input.
type Edit struct {
	Kind        EditKind
	Start       int
	End         int
	Replacement string
}

// EditKind identifies why bytes were inserted.
type EditKind string

const (
	EditEscapeQuote EditKind = "escape_quote"
	EditCloseString EditKind = "close_string"
	EditCloseObject EditKind = "close_object"
	EditCloseArray  EditKind = "close_array"
	EditInsertColon EditKind = "insert_colon"
	EditInsertComma EditKind = "insert_comma"
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
	inserted := 0
	previous := 0
	for _, edit := range edits {
		if edit.Start != edit.End || edit.Start < previous || edit.Start > len(input) {
			return nil, newError(ErrInvalidOutput, edit.Start)
		}
		inserted += len(edit.Replacement)
		if len(input)+inserted > maxOutputBytes {
			return nil, newError(ErrLimitExceeded, edit.Start)
		}
		previous = edit.Start
	}

	output := make([]byte, 0, len(input)+inserted)
	cursor := 0
	for _, edit := range edits {
		output = append(output, input[cursor:edit.Start]...)
		output = append(output, edit.Replacement...)
		cursor = edit.Start
	}
	output = append(output, input[cursor:]...)
	return output, nil
}
