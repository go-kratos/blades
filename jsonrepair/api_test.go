package jsonrepair

import (
	"strings"
	"testing"
)

func TestNewReturnsPermissiveEngine(t *testing.T) {
	t.Parallel()

	var engine *PermissiveEngine = New()
	result, err := engine.Repair([]byte(`{active: TRUE,}`))
	if err != nil {
		t.Fatalf("Repair() error = %v", err)
	}
	assertJSONSemanticallyEqual(t, result.JSON, []byte(`{"active":true}`))
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
	b.ResetTimer()
	for range b.N {
		if _, err := Repair(input); err != nil {
			b.Fatal(err)
		}
	}
}
