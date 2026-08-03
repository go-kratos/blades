// Package jsonrepair repairs malformed JSON by inserting missing syntax.
//
// The conservative repairer never deletes or replaces source bytes. Valid JSON
// is returned byte-for-byte unchanged, and every insertion made for malformed
// input is reported in Result.Edits.
package jsonrepair

import "encoding/json"

const (
	defaultMaxInputBytes  = 1 << 20
	defaultMaxOutputBytes = defaultMaxInputBytes + 64<<10
	defaultMaxDepth       = 128
	defaultMaxEdits       = 256
)

// Repairer repairs one complete sequence of received JSON bytes.
type Repairer interface {
	Repair(input []byte) (Result, error)
}

// Engine is an immutable repairer and is safe for concurrent use. Its zero
// value uses the default limits.
type Engine struct {
	limits Limits
}

// Option configures an Engine during New.
type Option func(*Engine)

// Limits bounds work performed on untrusted input. A non-positive field uses
// the package default for that field.
type Limits struct {
	MaxInputBytes  int
	MaxOutputBytes int
	MaxDepth       int
	MaxEdits       int
}

// New constructs a conservative repair engine.
func New(options ...Option) *Engine {
	engine := &Engine{}
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
	return func(engine *Engine) {
		engine.limits = limits
	}
}

// Func adapts a function to Repairer.
type Func func(input []byte) (Result, error)

// Repair calls f.
func (f Func) Repair(input []byte) (Result, error) {
	return f(input)
}

// Repair uses the default conservative engine.
func Repair(input []byte) (Result, error) {
	return Engine{}.Repair(input)
}

// Repair repairs input by inserting only syntax required for an unambiguous
// local recovery. It returns an error without partial JSON when repair fails.
func (engine Engine) Repair(input []byte) (Result, error) {
	limits := engine.effectiveLimits()
	if len(input) > limits.MaxInputBytes {
		return Result{}, newError(ErrLimitExceeded, limits.MaxInputBytes)
	}
	if len(input) > limits.MaxOutputBytes {
		return Result{}, newError(ErrLimitExceeded, limits.MaxOutputBytes)
	}
	if json.Valid(input) {
		return Result{JSON: append([]byte(nil), input...)}, nil
	}

	state := parser{input: input, limits: limits}
	if err := state.parse(); err != nil {
		return Result{}, err
	}
	output, err := applyEdits(input, state.edits, limits.MaxOutputBytes)
	if err != nil {
		return Result{}, err
	}
	if !json.Valid(output) {
		return Result{}, newError(ErrInvalidOutput, len(input))
	}

	return Result{
		JSON:  output,
		Edits: append([]Edit(nil), state.edits...),
	}, nil
}

func (engine Engine) effectiveLimits() Limits {
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
	if limits.MaxEdits <= 0 {
		limits.MaxEdits = defaultMaxEdits
	}
	return limits
}
