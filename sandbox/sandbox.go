package sandbox

import (
	"github.com/Use-Tusk/fence/pkg/fence"
)

// Option configures a Sandbox.
type Option func(*Sandbox)

// Sandbox wraps commands in an OS-level sandbox.
type Sandbox struct {
	config  Config
	manager *fence.Manager
	debug   bool
	monitor bool
}

// IsSupported returns true if the current platform supports sandboxing.
func IsSupported() bool {
	return fence.IsSupported()
}

// New creates a new Sandbox with the given options.
// The sandbox is lazily initialized on the first Wrap call.
func New(opts ...Option) *Sandbox {
	s := &Sandbox{}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Wrap wraps a command string with sandbox restrictions.
// The underlying sandbox infrastructure is lazily initialized on the first call.
func (s *Sandbox) Wrap(cmd string) (string, error) {
	if s.manager == nil {
		if err := s.init(); err != nil {
			return "", err
		}
	}
	return s.manager.WrapCommand(cmd)
}

// Close releases sandbox resources (proxies, bridges, etc.).
func (s *Sandbox) Close() {
	if s.manager != nil {
		s.manager.Cleanup()
	}
}

func (s *Sandbox) init() error {
	cfg := s.buildFenceConfig()
	s.manager = fence.NewManager(cfg, s.debug, s.monitor)
	return s.manager.Initialize()
}

func (s *Sandbox) buildFenceConfig() *fence.Config {
	return &fence.Config{
		Network: fence.NetworkConfig{
			AllowedDomains: s.config.Network.AllowedDomains,
			DeniedDomains:  s.config.Network.DeniedDomains,
		},
		Filesystem: fence.FilesystemConfig{
			AllowWrite: s.config.Filesystem.AllowWrite,
			DenyWrite:  s.config.Filesystem.DenyWrite,
			DenyRead:   s.config.Filesystem.DenyRead,
		},
		Command: fence.CommandConfig{
			Deny:  s.config.Command.Deny,
			Allow: s.config.Command.Allow,
		},
	}
}

// --- Network Options ---

// AllowDomains adds domains to the allow list.
func AllowDomains(domains ...string) Option {
	return func(s *Sandbox) {
		s.config.Network.AllowedDomains = append(s.config.Network.AllowedDomains, domains...)
	}
}

// DenyDomains adds domains to the deny list.
func DenyDomains(domains ...string) Option {
	return func(s *Sandbox) {
		s.config.Network.DeniedDomains = append(s.config.Network.DeniedDomains, domains...)
	}
}

// --- Filesystem Options ---

// AllowWrite adds writable paths.
func AllowWrite(paths ...string) Option {
	return func(s *Sandbox) {
		s.config.Filesystem.AllowWrite = append(s.config.Filesystem.AllowWrite, paths...)
	}
}

// DenyWrite adds paths denied for writing.
func DenyWrite(paths ...string) Option {
	return func(s *Sandbox) {
		s.config.Filesystem.DenyWrite = append(s.config.Filesystem.DenyWrite, paths...)
	}
}

// DenyRead adds paths denied for reading.
func DenyRead(paths ...string) Option {
	return func(s *Sandbox) {
		s.config.Filesystem.DenyRead = append(s.config.Filesystem.DenyRead, paths...)
	}
}

// --- Command Options ---

// DenyCommands adds commands to the deny list.
func DenyCommands(cmds ...string) Option {
	return func(s *Sandbox) {
		s.config.Command.Deny = append(s.config.Command.Deny, cmds...)
	}
}

// AllowCommands adds exceptions to deny rules.
func AllowCommands(cmds ...string) Option {
	return func(s *Sandbox) {
		s.config.Command.Allow = append(s.config.Command.Allow, cmds...)
	}
}

// --- Behavior Options ---

// WithDebug enables verbose debug logging.
func WithDebug(enabled bool) Option {
	return func(s *Sandbox) {
		s.debug = enabled
	}
}

// WithMonitor enables violation monitoring (logs only blocked requests).
func WithMonitor(enabled bool) Option {
	return func(s *Sandbox) {
		s.monitor = enabled
	}
}

// WithConfig sets a complete Config, replacing any previously set options.
func WithConfig(cfg Config) Option {
	return func(s *Sandbox) {
		s.config = cfg
	}
}
