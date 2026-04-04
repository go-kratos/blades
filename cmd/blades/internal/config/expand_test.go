package config

import (
	"os"
	"testing"
)

func TestExpandEnv(t *testing.T) {
	os.Setenv("TEST_VAR", "expanded")
	defer os.Unsetenv("TEST_VAR")

	if got := ExpandEnv("hello ${TEST_VAR}"); got != "hello expanded" {
		t.Errorf("ExpandEnv = %q, want %q", got, "hello expanded")
	}
	if got := ExpandEnv("unchanged"); got != "unchanged" {
		t.Errorf("ExpandEnv = %q, want unchanged", got)
	}
}
