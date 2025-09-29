package openai

import (
	"testing"
	"time"
)

func TestConfig(t *testing.T) {
	tests := []struct {
		name     string
		opts     []Option
		validate func(*testing.T, *Config)
	}{
		{
			name: "default configuration",
			opts: []Option{},
			validate: func(t *testing.T, c *Config) {
				if c.MaxRetries != 2 {
					t.Errorf("Expected MaxRetries 2, got %d", c.MaxRetries)
				}
				if c.Headers == nil {
					t.Error("Expected non-nil Headers")
				}
			},
		},
		{
			name: "with API key",
			opts: []Option{
				WithAPIKey("test-key"),
			},
			validate: func(t *testing.T, c *Config) {
				if c.APIKey != "test-key" {
					t.Errorf("Expected APIKey 'test-key', got '%s'", c.APIKey)
				}
			},
		},
		{
			name: "full configuration",
			opts: []Option{
				WithAPIKey("test-key"),
				WithBaseURL("https://api.example.com"),
				WithOrganization("org-123"),
				WithProject("proj-456"),
				WithRequestTimeout(60 * time.Second),
				WithMaxRetries(5),
				WithHeader("X-Custom", "value"),
			},
			validate: func(t *testing.T, c *Config) {
				if c.APIKey != "test-key" {
					t.Errorf("Expected APIKey 'test-key', got '%s'", c.APIKey)
				}
				if c.BaseURL != "https://api.example.com" {
					t.Errorf("Expected BaseURL 'https://api.example.com', got '%s'", c.BaseURL)
				}
				if c.Organization != "org-123" {
					t.Errorf("Expected Organization 'org-123', got '%s'", c.Organization)
				}
				if c.Project != "proj-456" {
					t.Errorf("Expected Project 'proj-456', got '%s'", c.Project)
				}
				if c.RequestTimeout != 60*time.Second {
					t.Errorf("Expected RequestTimeout 60s, got %v", c.RequestTimeout)
				}
				if c.MaxRetries != 5 {
					t.Errorf("Expected MaxRetries 5, got %d", c.MaxRetries)
				}
				if c.Headers["X-Custom"] != "value" {
					t.Errorf("Expected Header X-Custom 'value', got '%s'", c.Headers["X-Custom"])
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := NewConfig(tt.opts...)
			tt.validate(t, config)
		})
	}
}
