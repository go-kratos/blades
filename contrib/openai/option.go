package openai

import (
	"net/http"
	"time"

	"github.com/openai/openai-go/v2/option"
)

// Config wraps OpenAI provider configuration to avoid exposing external library types
type Config struct {
	APIKey         string
	BaseURL        string
	Organization   string
	Project        string
	HTTPClient     *http.Client
	RequestTimeout time.Duration
	MaxRetries     int
	Headers        map[string]string
}

// Option is a function type for configuring the provider
type Option func(*Config)

// WithAPIKey sets the API key
func WithAPIKey(apiKey string) Option {
	return func(c *Config) {
		c.APIKey = apiKey
	}
}

// WithBaseURL sets the base URL
func WithBaseURL(baseURL string) Option {
	return func(c *Config) {
		c.BaseURL = baseURL
	}
}

// WithOrganization sets the organization ID
func WithOrganization(org string) Option {
	return func(c *Config) {
		c.Organization = org
	}
}

// WithProject sets the project ID
func WithProject(project string) Option {
	return func(c *Config) {
		c.Project = project
	}
}

// WithHTTPClient sets a custom HTTP client
func WithHTTPClient(client *http.Client) Option {
	return func(c *Config) {
		c.HTTPClient = client
	}
}

// WithRequestTimeout sets the request timeout
func WithRequestTimeout(timeout time.Duration) Option {
	return func(c *Config) {
		c.RequestTimeout = timeout
	}
}

// WithMaxRetries sets the maximum number of retries
func WithMaxRetries(retries int) Option {
	return func(c *Config) {
		c.MaxRetries = retries
	}
}

// WithHeader sets a request header
func WithHeader(key, value string) Option {
	return func(c *Config) {
		if c.Headers == nil {
			c.Headers = make(map[string]string)
		}
		c.Headers[key] = value
	}
}

// NewConfig creates a new configuration with default values
func NewConfig(opts ...Option) *Config {
	config := &Config{
		Headers:    make(map[string]string),
		MaxRetries: 2, // OpenAI default
	}
	for _, opt := range opts {
		opt(config)
	}
	return config
}

// toRequestOptions converts the wrapped configuration to OpenAI RequestOptions
func (c *Config) toRequestOptions() []option.RequestOption {
	var opts []option.RequestOption

	if c.APIKey != "" {
		opts = append(opts, option.WithAPIKey(c.APIKey))
	}
	if c.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(c.BaseURL))
	}
	if c.Organization != "" {
		opts = append(opts, option.WithOrganization(c.Organization))
	}
	if c.Project != "" {
		opts = append(opts, option.WithProject(c.Project))
	}
	if c.HTTPClient != nil {
		opts = append(opts, option.WithHTTPClient(c.HTTPClient))
	}
	if c.RequestTimeout > 0 {
		opts = append(opts, option.WithRequestTimeout(c.RequestTimeout))
	}
	if c.MaxRetries >= 0 {
		opts = append(opts, option.WithMaxRetries(c.MaxRetries))
	}

	for key, value := range c.Headers {
		opts = append(opts, option.WithHeader(key, value))
	}

	return opts
}
