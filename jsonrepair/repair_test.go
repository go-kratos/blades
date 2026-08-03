package jsonrepair

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestRepairObservedFailureClasses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		expected  string
		editKinds []EditKind
	}{
		{
			name:      "truncated string and object",
			input:     `{"command":"第一行，\n第二行`,
			expected:  `{"command":"第一行，\n第二行"}`,
			editKinds: []EditKind{EditCloseString, EditCloseObject},
		},
		{
			name:      "missing outer object terminator",
			input:     `{"question":{"mode":"form","prompt":"请选择："}`,
			expected:  `{"question":{"mode":"form","prompt":"请选择："}}`,
			editKinds: []EditKind{EditCloseObject},
		},
		{
			name:      "unescaped quotes in string",
			input:     `{"question":{"prompt":"输入"已登录"后继续，或取消。"}}`,
			expected:  `{"question":{"prompt":"输入\"已登录\"后继续，或取消。"}}`,
			editKinds: []EditKind{EditEscapeQuote, EditEscapeQuote},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			result, err := Repair([]byte(test.input))
			if err != nil {
				t.Fatalf("Repair() error = %v", err)
			}
			if got := string(result.JSON); got != test.expected {
				t.Fatalf("Repair() JSON = %q, want %q", got, test.expected)
			}
			if !result.Changed() {
				t.Fatal("Result.Changed() = false, want true")
			}
			if got := editKinds(result.Edits); !equalEditKinds(got, test.editKinds) {
				t.Fatalf("edit kinds = %v, want %v", got, test.editKinds)
			}
			assertRepairInvariants(t, []byte(test.input), result)
		})
	}
}

func TestRepairLeavesValidInputUnchanged(t *testing.T) {
	t.Parallel()

	input := []byte(" {\n  \"prompt\": \"请选择：继续，或退出；\",\n  \"number\": 1.20e+3\n} ")
	result, err := Repair(input)
	if err != nil {
		t.Fatalf("Repair() error = %v", err)
	}
	if !bytes.Equal(result.JSON, input) {
		t.Fatalf("Repair() JSON = %q, want unchanged %q", result.JSON, input)
	}
	if result.Changed() {
		t.Fatalf("Result.Changed() = true, edits = %#v", result.Edits)
	}
	if result.Edits != nil {
		t.Fatalf("Result.Edits = %#v, want nil", result.Edits)
	}
	if len(result.JSON) > 0 {
		result.JSON[0] = 'x'
	}
	if input[0] != ' ' {
		t.Fatal("Repair() result aliases input")
	}
}

func TestRepairNestedContainersAndSeparators(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{name: "nested EOF", input: `[{"text":"value`, expected: `[{"text":"value"}]`},
		{name: "missing array close before ancestor", input: `{"values":[1}`, expected: `{"values":[1]}`},
		{name: "missing colon", input: `{"value" 1}`, expected: `{"value" :1}`},
		{name: "missing object comma", input: `{"first":1 "second":2}`, expected: `{"first":1 ,"second":2}`},
		{name: "missing array comma", input: `[1 2]`, expected: `[1 ,2]`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			result, err := Repair([]byte(test.input))
			if err != nil {
				t.Fatalf("Repair() error = %v", err)
			}
			if got := string(result.JSON); got != test.expected {
				t.Fatalf("Repair() JSON = %q, want %q", got, test.expected)
			}
			assertRepairInvariants(t, []byte(test.input), result)
		})
	}
}

func TestRepairRejectsUnsupportedOrAmbiguousInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  error
	}{
		{name: "empty", input: ``, want: ErrUnrepairable},
		{name: "comment", input: `{"value":1/* comment */}`, want: ErrUnrepairable},
		{name: "trailing comma", input: `{"value":1,}`, want: ErrUnrepairable},
		{name: "trailing comma after string", input: `{"value":"text",}`, want: ErrUnrepairable},
		{name: "single quotes", input: `{'value':'text'}`, want: ErrUnrepairable},
		{name: "raw control character", input: "{\"value\":\"first\nsecond\"}", want: ErrUnrepairable},
		{name: "invalid escape", input: `{"value":"secret\q"}`, want: ErrUnrepairable},
		{name: "ambiguous quote and missing comma", input: `{"first":"value" "second":2}`, want: ErrAmbiguous},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			result, err := Repair([]byte(test.input))
			if !errors.Is(err, test.want) {
				t.Fatalf("Repair() error = %v, want errors.Is(_, %v)", err, test.want)
			}
			if result.JSON != nil || result.Edits != nil {
				t.Fatalf("Repair() result = %#v, want empty result", result)
			}
		})
	}
}

func TestRepairLimits(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		limits Limits
		input  string
	}{
		{name: "input", limits: Limits{MaxInputBytes: 2}, input: `null`},
		{name: "output", limits: Limits{MaxOutputBytes: 1}, input: `{`},
		{name: "depth", limits: Limits{MaxDepth: 1}, input: `[[`},
		{name: "edits", limits: Limits{MaxEdits: 1}, input: `{"value":"text`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			engine := New(WithLimits(test.limits))
			_, err := engine.Repair([]byte(test.input))
			if !errors.Is(err, ErrLimitExceeded) {
				t.Fatalf("Repair() error = %v, want ErrLimitExceeded", err)
			}
		})
	}
}

func TestRepairErrorDoesNotExposeInput(t *testing.T) {
	t.Parallel()

	const secret = "credential-value-must-not-leak"
	_, err := Repair([]byte(`{"secret":"` + secret + `\q"}`))
	if err == nil {
		t.Fatal("Repair() error = nil, want error")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("Repair() error exposes input: %v", err)
	}
	var repairError *Error
	if !errors.As(err, &repairError) {
		t.Fatalf("Repair() error type = %T, want *Error", err)
	}
	if repairError.Offset < 0 {
		t.Fatalf("Repair() error offset = %d, want non-negative", repairError.Offset)
	}
}

func TestFuncImplementsRepairer(t *testing.T) {
	t.Parallel()

	var repairer Repairer = Func(func(input []byte) (Result, error) {
		return Result{JSON: append([]byte(nil), input...)}, nil
	})
	result, err := repairer.Repair([]byte(`{}`))
	if err != nil {
		t.Fatalf("Repair() error = %v", err)
	}
	if got := string(result.JSON); got != `{}` {
		t.Fatalf("Repair() JSON = %q, want %q", got, `{}`)
	}
}

func FuzzRepair(f *testing.F) {
	for _, seed := range []string{
		`{}`,
		`{"value":"text`,
		`{"question":{"prompt":"输入"已登录"后继续。"}}`,
		`[{"nested":true`,
		`{"value":1,}`,
	} {
		f.Add([]byte(seed))
	}

	f.Fuzz(func(t *testing.T, input []byte) {
		result, err := Repair(input)
		if err != nil {
			return
		}
		assertRepairInvariants(t, input, result)

		again, err := Repair(result.JSON)
		if err != nil {
			t.Fatalf("Repair(repaired) error = %v", err)
		}
		if !bytes.Equal(again.JSON, result.JSON) || again.Changed() {
			t.Fatalf("repair is not idempotent: first=%q second=%q edits=%#v", result.JSON, again.JSON, again.Edits)
		}
	})
}

func BenchmarkRepairValid(b *testing.B) {
	input := []byte(`{"items":[` + strings.Repeat(`{"text":"请选择：继续，或退出；"},`, 10_000) + `null]}`)
	b.ResetTimer()
	for range b.N {
		if _, err := Repair(input); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRepairQuoteHeavyInvalid(b *testing.B) {
	input := []byte(`{"text":"` + strings.Repeat(`输入"已登录"后继续，`, 10_000) + `结束"}`)
	engine := New(WithLimits(Limits{MaxEdits: 25_000}))
	b.ResetTimer()
	for range b.N {
		if _, err := engine.Repair(input); err != nil {
			b.Fatal(err)
		}
	}
}

func assertRepairInvariants(t *testing.T, input []byte, result Result) {
	t.Helper()
	if !json.Valid(result.JSON) {
		t.Fatalf("repaired JSON is invalid: input bytes=%d, output bytes=%d, edits=%d", len(input), len(result.JSON), len(result.Edits))
	}
	if !isSubsequence(input, result.JSON) {
		t.Fatalf("input is not a byte subsequence of output: input bytes=%d, output bytes=%d", len(input), len(result.JSON))
	}
	applied, err := applyEdits(input, result.Edits, len(result.JSON))
	if err != nil {
		t.Fatalf("applyEdits() error = %v", err)
	}
	if !bytes.Equal(applied, result.JSON) {
		t.Fatalf("applying edits did not reproduce output: applied bytes=%d, output bytes=%d", len(applied), len(result.JSON))
	}
}

func isSubsequence(input, output []byte) bool {
	position := 0
	for _, character := range output {
		if position < len(input) && character == input[position] {
			position++
		}
	}
	return position == len(input)
}

func editKinds(edits []Edit) []EditKind {
	kinds := make([]EditKind, len(edits))
	for index, edit := range edits {
		kinds[index] = edit.Kind
	}
	return kinds
}

func equalEditKinds(left, right []EditKind) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
