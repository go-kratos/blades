// Package jsonrepair recovers valid JSON from malformed model output.
//
// The package has one repair strategy. Repair and New use PermissiveEngine,
// which preserves valid JSON byte-for-byte and semantically recovers malformed
// input. A malformed repair may normalize, replace, or discard invalid syntax;
// the complete rewrite is reported in Result.Edits.
package jsonrepair

import (
	"bytes"
	"encoding/json"
	"unicode/utf8"
)

const (
	defaultMaxInputBytes  = 1 << 20
	defaultMaxOutputBytes = defaultMaxInputBytes + 64<<10
	defaultMaxDepth       = 128
)

// Repairer repairs one complete sequence of received JSON bytes.
type Repairer interface {
	Repair(input []byte) (Result, error)
}

// PermissiveEngine recovers a semantic JSON value from malformed input. Its
// zero value uses the default limits, and it is safe for concurrent use.
type PermissiveEngine struct {
	limits Limits
}

// Option configures a PermissiveEngine during New.
type Option func(*PermissiveEngine)

// Limits bounds work performed on untrusted input. A non-positive field uses
// the package default for that field.
type Limits struct {
	MaxInputBytes  int
	MaxOutputBytes int
	MaxDepth       int
}

// New constructs the package repair engine.
func New(options ...Option) *PermissiveEngine {
	engine := &PermissiveEngine{}
	for _, option := range options {
		if option != nil {
			option(engine)
		}
	}
	return engine
}

// WithLimits configures repair resource limits. Non-positive fields retain
// their default values.
func WithLimits(limits Limits) Option {
	return func(engine *PermissiveEngine) {
		engine.limits = limits
	}
}

// Func adapts a function to Repairer.
type Func func(input []byte) (Result, error)

// Repair calls f.
func (f Func) Repair(input []byte) (Result, error) {
	return f(input)
}

// Repair uses the default PermissiveEngine.
func Repair(input []byte) (Result, error) {
	return PermissiveEngine{}.Repair(input)
}

// Repair recovers and validates one JSON value. Valid input is returned
// byte-for-byte unchanged. Malformed input is parsed into a recovered value,
// serialized with encoding/json, and reported as one whole-document rewrite.
func (engine PermissiveEngine) Repair(input []byte) (Result, error) {
	limits := engine.effectiveLimits()
	if len(input) > limits.MaxInputBytes {
		return Result{}, newError(ErrLimitExceeded, limits.MaxInputBytes)
	}
	if len(input) > limits.MaxOutputBytes {
		return Result{}, newError(ErrLimitExceeded, limits.MaxOutputBytes)
	}
	if utf8.Valid(input) && json.Valid(input) {
		return Result{JSON: append([]byte(nil), input...)}, nil
	}

	state := newPermissiveParser(input, limits)
	value, err := state.parseDocument()
	if err != nil {
		return Result{}, err
	}
	output, err := json.Marshal(value)
	if err != nil {
		return Result{}, newError(ErrInvalidOutput, state.byteOffset(state.position))
	}
	if len(output) > limits.MaxOutputBytes {
		return Result{}, newError(ErrLimitExceeded, len(input))
	}

	edits := []Edit{{
		Kind:        EditRewriteDocument,
		Start:       0,
		End:         len(input),
		Replacement: string(output),
	}}
	applied, err := applyEdits(input, edits, limits.MaxOutputBytes)
	if err != nil {
		return Result{}, err
	}
	if !bytes.Equal(applied, output) || !json.Valid(applied) {
		return Result{}, newError(ErrInvalidOutput, len(input))
	}

	return Result{JSON: applied, Edits: edits}, nil
}

func (engine PermissiveEngine) effectiveLimits() Limits {
	limits := engine.limits
	if limits.MaxInputBytes <= 0 {
		limits.MaxInputBytes = defaultMaxInputBytes
	}
	if limits.MaxOutputBytes <= 0 {
		limits.MaxOutputBytes = defaultMaxOutputBytes
	}
	if limits.MaxDepth <= 0 {
		limits.MaxDepth = defaultMaxDepth
	}
	return limits
}
