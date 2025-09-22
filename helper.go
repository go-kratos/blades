package blades

import (
	"encoding/base64"
	"strings"
)

// extractPathFromURI extracts the path component from a URI, removing query parameters and fragments.
func extractPathFromURI(uri string) string {
	// Remove fragment (#)
	if idx := strings.Index(uri, "#"); idx != -1 {
		uri = uri[:idx]
	}
	// Remove query parameters (?)
	if idx := strings.Index(uri, "?"); idx != -1 {
		uri = uri[:idx]
	}
	return uri
}

// isBase64Data determines if the input string is base64 encoded data.
func isBase64Data(input string) bool {
	// Check for data URL format
	if strings.HasPrefix(input, "data:") {
		return true
	}

	// Check if it looks like base64 (rough heuristic)
	// Base64 strings are typically long and contain only valid base64 characters
	if len(input) < 20 {
		return false
	}

	// Check if it contains URL-like patterns
	if strings.Contains(input, "://") || strings.Contains(input, "/") {
		return false
	}

	// Try to decode as base64
	_, err := base64.StdEncoding.DecodeString(input)
	return err == nil
}

// extractBase64Data extracts base64 data and decodes it to bytes.
func extractBase64Data(input string) []byte {
	var base64Str string

	if strings.HasPrefix(input, "data:") {
		// Extract base64 part from data URL
		if idx := strings.Index(input, ","); idx != -1 {
			base64Str = input[idx+1:]
		} else {
			base64Str = input
		}
	} else {
		// Assume entire input is base64
		base64Str = input
	}

	data, err := base64.StdEncoding.DecodeString(base64Str)
	if err != nil {
		// If decoding fails, return empty bytes
		return []byte{}
	}

	return data
}

// extractMimeFromDataURL extracts MIME type from a data URL.
func extractMimeFromDataURL(dataURL string) MimeType {
	// Format: data:mime/type;base64,<data>
	if !strings.HasPrefix(dataURL, "data:") {
		return MimeImagePNG // default
	}

	// Find the first semicolon or comma
	start := 5 // len("data:")
	end := strings.IndexAny(dataURL[start:], ";,")
	if end == -1 {
		return MimeImagePNG // default
	}

	mimeStr := dataURL[start : start+end]
	return MimeType(mimeStr)
}