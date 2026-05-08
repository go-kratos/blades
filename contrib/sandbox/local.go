package sandbox

import (
	"github.com/Use-Tusk/fence/pkg/fence"
)

// NewLocalSandbox creates a new Sandbox with the given options.
// The sandbox is lazily initialized on the first WrapCmd call.
func NewLocalSandbox(opts ...Option) Sandbox {
	var cfg Config
	for _, opt := range opts {
		opt(&cfg)
	}
	return &localSandbox{config: cfg}
}

type localSandbox struct {
	config  Config
	manager *fence.Manager
}

func (s *localSandbox) WrapCmd(cmd string) (string, error) {
	if s.manager == nil {
		if err := s.init(); err != nil {
			return "", err
		}
	}
	return s.manager.WrapCommand(cmd)
}

func (s *localSandbox) Supported() bool {
	return fence.IsSupported()
}

func (s *localSandbox) Close() error {
	if s.manager != nil {
		s.manager.Cleanup()
	}
	return nil
}

func (s *localSandbox) init() error {
	cfg := s.buildFenceConfig()
	s.manager = fence.NewManager(cfg, false, false)
	return s.manager.Initialize()
}

func (s *localSandbox) buildFenceConfig() *fence.Config {
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
