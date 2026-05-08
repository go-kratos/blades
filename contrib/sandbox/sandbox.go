package sandbox

// Option configures a Sandbox.
type Option func(*Config)

// Config defines the sandbox policy.
type Config struct {
	Network    NetworkConfig
	Filesystem FilesystemConfig
	Command    CommandConfig
}

// NetworkConfig defines network restrictions.
type NetworkConfig struct {
	// AllowedDomains lists domains the sandbox may connect to.
	// Supports wildcards (e.g. "*.example.com").
	AllowedDomains []string
	// DeniedDomains lists domains explicitly blocked even if they match AllowedDomains.
	DeniedDomains []string
}

// FilesystemConfig defines filesystem restrictions.
type FilesystemConfig struct {
	// AllowWrite lists paths the sandbox may write to.
	AllowWrite []string
	// DenyWrite lists paths explicitly blocked from writing.
	DenyWrite []string
	// DenyRead lists paths explicitly blocked from reading.
	DenyRead []string
}

// CommandConfig defines command restrictions.
type CommandConfig struct {
	// Deny lists command patterns to block.
	Deny []string
	// Allow lists exceptions to deny rules.
	Allow []string
}

// AllowDomains adds domains to the allow list.
func AllowDomains(domains ...string) Option {
	return func(c *Config) {
		c.Network.AllowedDomains = append(c.Network.AllowedDomains, domains...)
	}
}

// DenyDomains adds domains to the deny list.
func DenyDomains(domains ...string) Option {
	return func(c *Config) {
		c.Network.DeniedDomains = append(c.Network.DeniedDomains, domains...)
	}
}

// AllowWrite adds writable paths.
func AllowWrite(paths ...string) Option {
	return func(c *Config) {
		c.Filesystem.AllowWrite = append(c.Filesystem.AllowWrite, paths...)
	}
}

// DenyWrite adds paths denied for writing.
func DenyWrite(paths ...string) Option {
	return func(c *Config) {
		c.Filesystem.DenyWrite = append(c.Filesystem.DenyWrite, paths...)
	}
}

// DenyRead adds paths denied for reading.
func DenyRead(paths ...string) Option {
	return func(c *Config) {
		c.Filesystem.DenyRead = append(c.Filesystem.DenyRead, paths...)
	}
}

// DenyCommands adds commands to the deny list.
func DenyCommands(cmds ...string) Option {
	return func(c *Config) {
		c.Command.Deny = append(c.Command.Deny, cmds...)
	}
}

// AllowCommands adds exceptions to deny rules.
func AllowCommands(cmds ...string) Option {
	return func(c *Config) {
		c.Command.Allow = append(c.Command.Allow, cmds...)
	}
}

// Sandbox wraps commands in an OS-level sandbox.
type Sandbox interface {
	// WrapCmd wraps a command string with sandbox restrictions.
	WrapCmd(cmd string) (string, error)
	// Supported reports whether sandboxing is available on the current platform.
	Supported() bool
	// Close releases sandbox resources.
	Close() error
}
