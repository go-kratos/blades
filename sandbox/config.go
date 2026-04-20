package sandbox

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
