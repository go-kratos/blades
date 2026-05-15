package model

import (
	"testing"
	"time"
)

func TestMergeOptionsKeepsCacheHintsByScope(t *testing.T) {
	t.Parallel()

	defaults := []Option{
		CacheHint{Scope: CacheScopeSystem, TTL: time.Minute},
		CacheHint{Scope: CacheScopeTool, TTL: 2 * time.Minute},
	}
	request := []Option{
		CacheHint{Scope: CacheScopeSystem, TTL: 3 * time.Minute},
	}

	got := MergeOptions(defaults, request)
	if len(got) != 2 {
		t.Fatalf("len(MergeOptions) = %d, want 2", len(got))
	}

	first, ok := got[0].(CacheHint)
	if !ok {
		t.Fatalf("first option type = %T, want CacheHint", got[0])
	}
	if first.Scope != CacheScopeSystem || first.TTL != 3*time.Minute {
		t.Fatalf("first option = %#v, want request system cache hint", first)
	}

	second, ok := got[1].(CacheHint)
	if !ok {
		t.Fatalf("second option type = %T, want CacheHint", got[1])
	}
	if second.Scope != CacheScopeTool || second.TTL != 2*time.Minute {
		t.Fatalf("second option = %#v, want default tool cache hint", second)
	}
}
