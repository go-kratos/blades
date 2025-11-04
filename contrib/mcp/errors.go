package mcp

import "errors"

var (
	// ErrNotConnected indicates the client is not connected to the server
	ErrNotConnected = errors.New("mcp: not connected")
	// ErrToolNotFound indicates the requested tool does not exist
	ErrToolNotFound = errors.New("mcp: tool not found")
	// ErrInvalidResponse indicates the server returned an invalid response
	ErrInvalidResponse = errors.New("mcp: invalid response")
	// ErrTransportFailed indicates a transport-level failure
	ErrTransportFailed = errors.New("mcp: transport failed")
)
