package jsonrepair

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func TestRepairRecoveryClasses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input []byte
		want  string
	}{
		{
			name:  "wrapper and trailing separator",
			input: []byte("result follows:\n```json\n{\"region\":\"north\",}\n```"),
			want:  `{"region":"north"}`,
		},
		{
			name:  "alternate quotes bare fields and literals",
			input: []byte(`{'ready': TRUE, ratio: .75, label: north wing,}`),
			want:  `{"label":"north wing","ratio":0.75,"ready":true}`,
		},
		{
			name:  "truncated nested value",
			input: []byte(`{"nodes":[{"id":7},{"id":8`),
			want:  `{"nodes":[{"id":7},{"id":8}]}`,
		},
		{
			name:  "unescaped quotes inside a value",
			input: []byte(`{"message":"tap "Continue",then", "ok":true}`),
			want:  `{"message":"tap \"Continue\",then","ok":true}`,
		},
		{
			name:  "non structural annotations",
			input: []byte(`{"first":1, NOTE "second":"two" discard-this }`),
			want:  `{"first":1,"second":"two"}`,
		},
		{
			name:  "unicode delimiters",
			input: []byte(`｛＂label＂：＂西＂，＂items＂：［“甲”，“乙”］｝`),
			want:  `{"items":["甲","乙"],"label":"西"}`,
		},
		{
			name:  "invalid source byte",
			input: []byte{'{', '"', 'x', '"', ':', 0xff, '}'},
			want:  `{"x":""}`,
		},
		{
			name:  "invalid byte inside string",
			input: []byte{'{', '"', 'x', '"', ':', '"', 'a', 0xff, 'b', '"', '}'},
			want:  `{"x":"ab"}`,
		},
		{
			name:  "multiple documents",
			input: []byte(`{"alpha":1} [false]`),
			want:  `[{"alpha":1},[false]]`,
		},
		{
			name:  "wrong array closer",
			input: []byte(`{"rows":[{"n":1}}],"tail":"yes"}`),
			want:  `{"rows":[{"n":1}],"tail":"yes"}`,
		},
		{
			name:  "redundant quote",
			input: []byte(`{"title": ""hello"}`),
			want:  `{"title":"hello"}`,
		},
		{
			name:  "invalid escape and raw key newline",
			input: []byte("{\"bad\\_key\n\":\"line\\q\"}"),
			want:  `{"bad_key":"lineq"}`,
		},
		{
			name:  "adjacent key and value",
			input: []byte(`[{"salute""bonjour"}]`),
			want:  `[{"salute":"bonjour"}]`,
		},
		{
			name:  "missing primitive separators",
			input: []byte(`{"left" 1 "right":2,"sequence":[3 4]}`),
			want:  `{"left":1,"right":2,"sequence":[3,4]}`,
		},
		{
			name:  "missing string separators",
			input: []byte(`{"first":"red" "second":"blue","tones":["warm" "cool"]}`),
			want:  `{"first":"red","second":"blue","tones":["warm","cool"]}`,
		},
		{
			name:  "incomplete empty array item",
			input: []byte("[  \n  \""),
			want:  `[]`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			result, err := Repair(test.input)
			if err != nil {
				t.Fatalf("Repair() error = %v", err)
			}
			assertJSONSemanticallyEqual(t, result.JSON, []byte(test.want))
			assertRepairInvariants(t, test.input, result)
		})
	}
}

func TestRepairLeavesValidInputUnchanged(t *testing.T) {
	t.Parallel()

	input := []byte(" \n{\"value\":1.00,\"enabled\":true}\t")
	result, err := Repair(input)
	if err != nil {
		t.Fatalf("Repair() error = %v", err)
	}
	if !bytes.Equal(result.JSON, input) {
		t.Fatalf("Repair() JSON = %q, want unchanged %q", result.JSON, input)
	}
	if result.Changed() || result.Edits != nil {
		t.Fatalf("Repair() edits = %#v, want nil", result.Edits)
	}
	result.JSON[0] = 'x'
	if input[0] != ' ' {
		t.Fatal("Repair() result aliases input")
	}
}

func TestRepairReportsDocumentRewrite(t *testing.T) {
	t.Parallel()

	input := []byte(`{name:'Ada',}`)
	result, err := Repair(input)
	if err != nil {
		t.Fatalf("Repair() error = %v", err)
	}
	if len(result.Edits) != 1 {
		t.Fatalf("len(Result.Edits) = %d, want 1", len(result.Edits))
	}
	edit := result.Edits[0]
	if edit.Kind != EditRewriteDocument || edit.Start != 0 || edit.End != len(input) || edit.Replacement != string(result.JSON) {
		t.Fatalf("Result.Edits[0] = %#v, want a complete rewrite", edit)
	}
	applied, err := applyEdits(input, result.Edits, len(result.JSON))
	if err != nil {
		t.Fatalf("applyEdits() error = %v", err)
	}
	if !bytes.Equal(applied, result.JSON) {
		t.Fatalf("applyEdits() = %q, want %q", applied, result.JSON)
	}
}

func TestRepairLimits(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		limits Limits
		input  []byte
	}{
		{
			name:   "input",
			limits: Limits{MaxInputBytes: 4},
			input:  []byte(`{"wide":1}`),
		},
		{
			name:   "output expansion",
			limits: Limits{MaxOutputBytes: len("{\"v\":\"\n\"}")},
			input:  []byte("{\"v\":\"\n\"}"),
		},
		{
			name:   "depth",
			limits: Limits{MaxDepth: 2},
			input:  []byte(`[[[`),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			engine := New(WithLimits(test.limits))
			_, err := engine.Repair(test.input)
			if !errors.Is(err, ErrLimitExceeded) {
				t.Fatalf("Repair() error = %v, want ErrLimitExceeded", err)
			}
		})
	}
}

func TestRepairErrorUsesOriginalByteOffset(t *testing.T) {
	t.Parallel()

	input := []byte(`{"西":[[[`)
	engine := New(WithLimits(Limits{MaxDepth: 2}))
	_, err := engine.Repair(input)
	if !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("Repair() error = %v, want ErrLimitExceeded", err)
	}
	var repairError *Error
	if !errors.As(err, &repairError) {
		t.Fatalf("Repair() error type = %T, want *Error", err)
	}
	wantOffset := bytes.Index(input, []byte(`[[[`)) + 1
	if repairError.Offset != wantOffset {
		t.Fatalf("Repair() error offset = %d, want byte offset %d", repairError.Offset, wantOffset)
	}
}

func TestPermissiveEngineImplementsRepairer(t *testing.T) {
	t.Parallel()

	var repairer Repairer = New()
	result, err := repairer.Repair([]byte(`{active: TRUE}`))
	if err != nil {
		t.Fatalf("Repair() error = %v", err)
	}
	assertJSONSemanticallyEqual(t, result.JSON, []byte(`{"active":true}`))
}

func FuzzRepair(f *testing.F) {
	for _, seed := range [][]byte{
		[]byte(`{label:'west',}`),
		[]byte("```json\n[TRUE, .5,\n```"),
		{'{', '"', 'x', '"', ':', 0xfe, '}'},
		[]byte(`{"text":"select "one" now"}`),
	} {
		f.Add(seed)
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

func assertRepairInvariants(t *testing.T, input []byte, result Result) {
	t.Helper()
	if !json.Valid(result.JSON) {
		t.Fatalf("repaired JSON is invalid: input bytes=%d, output bytes=%d", len(input), len(result.JSON))
	}
	applied, err := applyEdits(input, result.Edits, len(result.JSON))
	if err != nil {
		t.Fatalf("applyEdits() error = %v", err)
	}
	if !bytes.Equal(applied, result.JSON) {
		t.Fatalf("applying edits produced %q, want %q", applied, result.JSON)
	}
}

func assertJSONSemanticallyEqual(t *testing.T, got, want []byte) {
	t.Helper()
	var gotValue any
	if err := json.Unmarshal(got, &gotValue); err != nil {
		t.Fatalf("json.Unmarshal(got) error = %v; JSON = %q", err, got)
	}
	var wantValue any
	if err := json.Unmarshal(want, &wantValue); err != nil {
		t.Fatalf("json.Unmarshal(want) error = %v; JSON = %q", err, want)
	}
	if !reflect.DeepEqual(gotValue, wantValue) {
		t.Fatalf("repaired value = %#v, want %#v; JSON = %q", gotValue, wantValue, got)
	}
}
