package sandbox

import (
	"testing"
)

// asFence extracts the underlying *fenceSandbox for internal field inspection.
func asFence(s Sandbox) *localSandbox {
	return s.(*localSandbox)
}

func TestNewLocalSandbox_defaults(t *testing.T) {
	sb := NewLocalSandbox()
	defer sb.Close()
	f := asFence(sb)
	if len(f.config.Network.AllowedDomains) != 0 {
		t.Error("allowed domains should be empty by default")
	}
}

func TestAllowDomains_accumulates(t *testing.T) {
	sb := NewLocalSandbox(
		AllowDomains("a.com"),
		AllowDomains("b.com", "c.com"),
	)
	defer sb.Close()
	got := asFence(sb).config.Network.AllowedDomains
	want := []string{"a.com", "b.com", "c.com"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestDenyDomains(t *testing.T) {
	sb := NewLocalSandbox(DenyDomains("evil.com"))
	defer sb.Close()
	f := asFence(sb)
	if len(f.config.Network.DeniedDomains) != 1 || f.config.Network.DeniedDomains[0] != "evil.com" {
		t.Errorf("unexpected denied domains: %v", f.config.Network.DeniedDomains)
	}
}

func TestFilesystemOptions(t *testing.T) {
	sb := NewLocalSandbox(
		AllowWrite(".", "/tmp"),
		DenyWrite("/etc"),
		DenyRead("~/.ssh"),
	)
	defer sb.Close()
	f := asFence(sb)
	if len(f.config.Filesystem.AllowWrite) != 2 {
		t.Errorf("unexpected allow write: %v", f.config.Filesystem.AllowWrite)
	}
	if len(f.config.Filesystem.DenyWrite) != 1 {
		t.Errorf("unexpected deny write: %v", f.config.Filesystem.DenyWrite)
	}
	if len(f.config.Filesystem.DenyRead) != 1 {
		t.Errorf("unexpected deny read: %v", f.config.Filesystem.DenyRead)
	}
}

func TestCommandOptions(t *testing.T) {
	sb := NewLocalSandbox(
		DenyCommands("rm -rf /"),
		AllowCommands("rm -rf /tmp"),
	)
	defer sb.Close()
	f := asFence(sb)
	if len(f.config.Command.Deny) != 1 || f.config.Command.Deny[0] != "rm -rf /" {
		t.Errorf("unexpected deny commands: %v", f.config.Command.Deny)
	}
	if len(f.config.Command.Allow) != 1 || f.config.Command.Allow[0] != "rm -rf /tmp" {
		t.Errorf("unexpected allow commands: %v", f.config.Command.Allow)
	}
}

func TestBuildFenceConfig(t *testing.T) {
	sb := NewLocalSandbox(
		AllowDomains("example.com"),
		DenyDomains("evil.com"),
		AllowWrite("."),
		DenyWrite("/etc"),
		DenyRead("~/.ssh"),
		DenyCommands("rm -rf /"),
		AllowCommands("rm -rf /tmp"),
	)
	defer sb.Close()
	cfg := asFence(sb).buildFenceConfig()
	if len(cfg.Network.AllowedDomains) != 1 {
		t.Errorf("fence config allowed domains: %v", cfg.Network.AllowedDomains)
	}
	if len(cfg.Network.DeniedDomains) != 1 {
		t.Errorf("fence config denied domains: %v", cfg.Network.DeniedDomains)
	}
	if len(cfg.Filesystem.AllowWrite) != 1 {
		t.Errorf("fence config allow write: %v", cfg.Filesystem.AllowWrite)
	}
	if len(cfg.Filesystem.DenyWrite) != 1 {
		t.Errorf("fence config deny write: %v", cfg.Filesystem.DenyWrite)
	}
	if len(cfg.Filesystem.DenyRead) != 1 {
		t.Errorf("fence config deny read: %v", cfg.Filesystem.DenyRead)
	}
	if len(cfg.Command.Deny) != 1 {
		t.Errorf("fence config deny commands: %v", cfg.Command.Deny)
	}
	if len(cfg.Command.Allow) != 1 {
		t.Errorf("fence config allow commands: %v", cfg.Command.Allow)
	}
}

func TestSupported(t *testing.T) {
	sb := NewLocalSandbox()
	defer sb.Close()
	// Just verify it doesn't panic; actual value depends on platform.
	_ = sb.Supported()
}
