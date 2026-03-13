package blades

import "testing"

func TestMergeParts(t *testing.T) {
	t.Parallel()

	t.Run("nil base returns extra", func(t *testing.T) {
		t.Parallel()

		extra := AssistantMessage("extra")
		got := MergeParts(nil, extra)
		if got != extra {
			t.Fatalf("MergeParts(nil, extra) = %p, want %p", got, extra)
		}
	})

	t.Run("nil extra keeps base", func(t *testing.T) {
		t.Parallel()

		base := AssistantMessage("base")
		got := MergeParts(base, nil)
		if got != base {
			t.Fatalf("MergeParts(base, nil) = %p, want %p", got, base)
		}
	})
}
