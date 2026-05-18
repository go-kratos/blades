package mcp

import "net/http"

type headerRoundTripper struct {
	base    http.RoundTripper
	headers map[string]string
}

func newHeaderRoundTripper(headers map[string]string, base http.RoundTripper) http.RoundTripper {
	if len(headers) == 0 {
		if base == nil {
			return http.DefaultTransport
		}
		return base
	}
	if base == nil {
		base = http.DefaultTransport
	}
	return &headerRoundTripper{
		base:    base,
		headers: headers,
	}
}

func (rt *headerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	for k, v := range rt.headers {
		if k != "" {
			req.Header.Set(k, v)
		}
	}
	return rt.base.RoundTrip(req)
}
