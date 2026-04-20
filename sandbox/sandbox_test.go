package sandbox

import (
	"testing"
)

func TestNew_defaults(t *testing.T) {
	sb := New()
	defer sb.Close()
	if sb.debug {
		t.Error("debug should default to false")
	}
	if sb.monitor {
		t.Error("monitor should default to false")
	}
	if len(sb.config.Network.AllowedDomains) != 0 {
		t.Error("allowed domains should be empty by default")
	}
}

func TestAllowDomains_accumulates(t *testing.T) {
	sb := New(
		AllowDomains("a.com"),
		AllowDomains("b.com", "c.com"),
	)
	defer sb.Close()
	got := sb.config.Network.AllowedDomains
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
	sb := New(DenyDomains("evil.com"))
	defer sb.Close()
	if len(sb.config.Network.DeniedDomains) != 1 || sb.config.Network.DeniedDomains[0] != "evil.com" {
		t.Errorf("unexpected denied domains: %v", sb.config.Network.DeniedDomains)
	}
}

func TestFilesystemOptions(t *testing.T) {
	sb := New(
		AllowWrite(".", "/tmp"),
		DenyWrite("/etc"),
		DenyRead("~/.ssh"),
	)
	defer sb.Close()
	if len(sb.config.Filesystem.AllowWrite) != 2 {
		t.Errorf("unexpected allow write: %v", sb.config.Filesystem.AllowWrite)
	}
	if len(sb.config.Filesystem.DenyWrite) != 1 {
		t.Errorf("unexpected deny write: %v", sb.config.Filesystem.DenyWrite)
	}
	if len(sb.config.Filesystem.DenyRead) != 1 {
		t.Errorf("unexpected deny read: %v", sb.config.Filesystem.DenyRead)
	}
}

func TestCommandOptions(t *testing.T) {
	sb := New(
		DenyCommands("rm -rf /"),
		AllowCommands("rm -rf /tmp"),
	)
	defer sb.Close()
	if len(sb.config.Command.Deny) != 1 || sb.config.Command.Deny[0] != "rm -rf /" {
		t.Errorf("unexpected deny commands: %v", sb.config.Command.Deny)
	}
	if len(sb.config.Command.Allow) != 1 || sb.config.Command.Allow[0] != "rm -rf /tmp" {
		t.Errorf("unexpected allow commands: %v", sb.config.Command.Allow)
	}
}

func TestWithConfig_replaces(t *testing.T) {
	sb := New(
		AllowDomains("first.com"),
		WithConfig(Config{
			Network: NetworkConfig{AllowedDomains: []string{"second.com"}},
		}),
	)
	defer sb.Close()
	got := sb.config.Network.AllowedDomains
	if len(got) != 1 || got[0] != "second.com" {
		t.Errorf("WithConfig should replace: got %v", got)
	}
}

func TestWithDebug(t *testing.T) {
	sb := New(WithDebug(true))
	defer sb.Close()
	if !sb.debug {
		t.Error("debug should be true")
	}
}

func TestWithMonitor(t *testing.T) {
	sb := New(WithMonitor(true))
	defer sb.Close()
	if !sb.monitor {
		t.Error("monitor should be true")
	}
}

func TestBuildFenceConfig(t *testing.T) {
	sb := New(
		AllowDomains("example.com"),
		DenyDomains("evil.com"),
		AllowWrite("."),
		DenyWrite("/etc"),
		DenyRead("~/.ssh"),
		DenyCommands("rm -rf /"),
		AllowCommands("rm -rf /tmp"),
	)
	defer sb.Close()
	cfg := sb.buildFenceConfig()
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

func TestIsSupported(t *testing.T) {
	// Just verify it doesn't panic; actual value depends on platform.
	_ = IsSupported()
}
