package app

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-kratos/blades/cmd/blades/internal/workspace"
)

func TestBuildDoctorContextUsesExplicitConfigPath(t *testing.T) {
	home := withTempHome(t)
	ws := workspace.NewWithWorkspace(filepath.Join(home, ".blades"), filepath.Join(home, "agent"))
	if err := ws.InitHome(); err != nil {
		t.Fatalf("InitHome: %v", err)
	}
	if err := ws.InitWorkspace(); err != nil {
		t.Fatalf("InitWorkspace: %v", err)
	}

	customConfig := filepath.Join(t.TempDir(), "custom-config.yaml")
	if err := os.WriteFile(customConfig, []byte("providers: []\n"), 0o644); err != nil {
		t.Fatalf("write custom config: %v", err)
	}

	ctx := BuildDoctorContext(Options{
		ConfigPath:   customConfig,
		WorkspaceDir: ws.WorkspaceDir(),
	})
	if got, want := ctx.ConfigPath, customConfig; got != want {
		t.Fatalf("ConfigPath = %q, want %q", got, want)
	}
}

func TestRunDoctorChecksAndPrintDoctorResults(t *testing.T) {
	results, err := RunDoctorChecks(&DoctorContext{}, []DoctorCheck{
		{
			Name: "first",
			Run: func(*DoctorContext) ([]DoctorResult, error) {
				return []DoctorResult{{Label: "one", Detail: "ok", OK: true}}, nil
			},
		},
		{
			Name: "second",
			Run: func(*DoctorContext) ([]DoctorResult, error) {
				return []DoctorResult{
					{Label: "two", Detail: "warn", OK: false},
					{Label: "stale", Detail: "job-x", OK: false},
				}, nil
			},
		},
	})
	if err != nil {
		t.Fatalf("RunDoctorChecks: %v", err)
	}
	if got, want := len(results), 3; got != want {
		t.Fatalf("len(results) = %d, want %d", got, want)
	}

	var buf bytes.Buffer
	ok := PrintDoctorResults(&buf, results)
	if ok {
		t.Fatal("PrintDoctorResults should report failure")
	}
	out := buf.String()
	if !strings.Contains(out, "one") || !strings.Contains(out, "warn") || !strings.Contains(out, "stale: job-x") {
		t.Fatalf("PrintDoctorResults output = %q", out)
	}
}
