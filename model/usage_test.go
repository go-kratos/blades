package model

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestUsageJSONFieldsOmitEmpty(t *testing.T) {
	t.Parallel()

	usageType := reflect.TypeOf(Usage{})
	for i := range usageType.NumField() {
		field := usageType.Field(i)
		tag, ok := field.Tag.Lookup("json")
		if !ok || !strings.Contains(","+tag+",", ",omitempty,") {
			t.Errorf("Usage.%s JSON tag = %q, want omitempty", field.Name, tag)
		}
	}

	raw, err := json.Marshal(Usage{})
	if err != nil {
		t.Fatalf("marshal zero Usage: %v", err)
	}
	if got, want := string(raw), `{}`; got != want {
		t.Fatalf("marshal zero Usage = %s, want %s", got, want)
	}

	raw, err = json.Marshal(Usage{TotalTokens: 7})
	if err != nil {
		t.Fatalf("marshal populated Usage: %v", err)
	}
	if got, want := string(raw), `{"totalTokens":7}`; got != want {
		t.Fatalf("marshal populated Usage = %s, want %s", got, want)
	}
}
