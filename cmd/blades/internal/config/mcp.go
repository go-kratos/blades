package config

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"time"

	bladesmcp "github.com/go-kratos/blades/contrib/mcp"
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
// Supports aliases: "type" for "transport", "url" for "endpoint".
type mcpServerEntry struct {
	// Transport is one of: stdio (default), http, websocket.
	Transport string `json:"transport"`
	// Type is an alias for Transport (e.g. Claude Desktop / custom configs).
	Type string `json:"type"`
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
	// URL is an alias for Endpoint.
	URL string `json:"url"`
	// Headers are custom HTTP headers (http transport).
	Headers map[string]string `json:"headers"`
	// TimeoutSeconds overrides the default 30-second request timeout.
	TimeoutSeconds int `json:"timeoutSeconds"`
}

func (e mcpServerEntry) toClientConfig(name string) bladesmcp.ClientConfig {
	transport := e.Transport
	if transport == "" {
		transport = e.Type
	}
	if transport == "" {
		transport = "stdio"
	}
	endpoint := e.Endpoint
	if endpoint == "" {
		endpoint = e.URL
	}
	cc := bladesmcp.ClientConfig{
		Name:      name,
		Transport: bladesmcp.TransportType(transport),
		Command:   e.Command,
		Args:      e.Args,
		Env:       e.Env,
		WorkDir:   e.WorkDir,
		Endpoint:  endpoint,
		Headers:   e.Headers,
	}
	if e.TimeoutSeconds > 0 {
		cc.Timeout = time.Duration(e.TimeoutSeconds) * time.Second
	}
	return cc
}

// LoadMCPFile reads path and returns the servers as []bladesmcp.ClientConfig.
// Returns nil, nil when the file does not exist (not an error).
// Environment variable references in string values (${VAR}) are expanded.
// Supports both "transport"/"endpoint" and "type"/"url" field names.
func LoadMCPFile(path string) ([]bladesmcp.ClientConfig, error) {
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

	out := make([]bladesmcp.ClientConfig, 0, len(f.MCPServers))
	names := make([]string, 0, len(f.MCPServers))
	for name := range f.MCPServers {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		e := f.MCPServers[name]
		out = append(out, e.toClientConfig(name))
	}
	return out, nil
}
