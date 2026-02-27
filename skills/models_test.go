package skills

import (
	"strings"
	"testing"
)

func TestFrontmatterValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		fm      Frontmatter
		wantErr bool
	}{
		{
			name: "valid",
			fm: Frontmatter{
				Name:        "my-skill",
				Description: "desc",
			},
			wantErr: false,
		},
		{
			name: "invalid name",
			fm: Frontmatter{
				Name:        "My-Skill",
				Description: "desc",
			},
			wantErr: true,
		},
		{
			name: "empty description",
			fm: Frontmatter{
				Name:        "my-skill",
				Description: "",
			},
			wantErr: true,
		},
		{
			name: "too long compatibility",
			fm: Frontmatter{
				Name:          "my-skill",
				Description:   "desc",
				Compatibility: strings.Repeat("x", 501),
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.fm.Validate()
			if tc.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestResourcesList(t *testing.T) {
	t.Parallel()

	r := Resources{
		References: map[string]string{"b.md": "b", "a.md": "a"},
		Assets:     map[string]string{"x.txt": "x"},
		Scripts:    map[string]string{"run.sh": "echo hi"},
	}
	refs := r.ListReferences()
	if len(refs) != 2 || refs[0] != "a.md" || refs[1] != "b.md" {
		t.Fatalf("unexpected refs: %v", refs)
	}
	if _, ok := r.GetAsset("x.txt"); !ok {
		t.Fatalf("expected asset x.txt")
	}
	if _, ok := r.GetScript("run.sh"); !ok {
		t.Fatalf("expected script run.sh")
	}
}
