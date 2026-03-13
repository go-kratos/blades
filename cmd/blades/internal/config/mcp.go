package config

import (
	"encoding/json"
	"fmt"
	"os"
)

// mcpFile is the on-disk representation of mcp.json.
// The top-level key "mcpServers" mirrors the Claude Desktop config format so
// that existing server definitions can be reused without modification.
//
// Example mcp.json:
//
//	{
//	  "mcpServers": {
//	    "time": {
//	      "command": "npx",
//	      "args": ["-y", "@modelcontextprotocol/server-time"]
//	    },
//	    "filesystem": {
//	      "command": "npx",
//	      "args": ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"],
//	      "env": { "DEBUG": "1" }
//	    },
//	    "my-api": {
//	      "transport": "http",
//	      "endpoint": "http://localhost:8080/mcp",
//	      "headers": { "Authorization": "Bearer ${MY_TOKEN}" },
//	      "timeoutSeconds": 15
//	    }
//	  }
//	}
type mcpFile struct {
	MCPServers map[string]mcpServerEntry `json:"mcpServers"`
}

// mcpServerEntry is a single server definition inside mcp.json.
// Fields default to stdio transport when "transport" is omitted.
type mcpServerEntry struct {
	// Transport is one of: stdio (default), http, websocket.
	Transport string `json:"transport"`
	// Command is the executable to launch (stdio transport).
	Command string `json:"command"`
	// Args are the command-line arguments (stdio transport).
	Args []string `json:"args"`
	// Env contains extra environment variables (stdio transport).
	Env map[string]string `json:"env"`
	// WorkDir is the working directory (stdio transport).
	WorkDir string `json:"workDir"`
	// Endpoint is the server URL (http / websocket transport).
	Endpoint string `json:"endpoint"`
	// Headers are custom HTTP headers (http transport).
	Headers map[string]string `json:"headers"`
	// TimeoutSeconds overrides the default 30-second request timeout.
	TimeoutSeconds int `json:"timeoutSeconds"`
}

// LoadMCPFile reads path and returns the servers as []MCPServerConfig.
// Returns nil, nil when the file does not exist (not an error).
// Environment variable references in string values (${VAR}) are expanded.
func LoadMCPFile(path string) ([]MCPServerConfig, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("mcp: read %q: %w", path, err)
	}

	expanded := os.ExpandEnv(string(data))

	var f mcpFile
	if err := json.Unmarshal([]byte(expanded), &f); err != nil {
		return nil, fmt.Errorf("mcp: parse %q: %w", path, err)
	}

	out := make([]MCPServerConfig, 0, len(f.MCPServers))
	for name, e := range f.MCPServers {
		transport := e.Transport
		if transport == "" {
			transport = "stdio"
		}
		out = append(out, MCPServerConfig{
			Name:           name,
			Transport:      transport,
			Command:        e.Command,
			Args:           e.Args,
			Env:            e.Env,
			WorkDir:        e.WorkDir,
			Endpoint:       e.Endpoint,
			Headers:        e.Headers,
			TimeoutSeconds: e.TimeoutSeconds,
		})
	}
	return out, nil
}
