package config

import "os"

// ExpandEnv replaces ${VAR} and $VAR in s with the value of the named
// environment variable. Used consistently for config.yaml templates.
func ExpandEnv(s string) string {
	return os.ExpandEnv(s)
}
