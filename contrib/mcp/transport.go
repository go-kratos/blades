package mcp

import "net/http"

type headerPair struct {
	key   string
	value string
}

type headerRoundTripper struct {
	base    http.RoundTripper
	headers []headerPair
}

func newHeaderRoundTripper(headers map[string]string, base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	pairs := make([]headerPair, 0, len(headers))
	for k, v := range headers {
		if k == "" {
			continue
		}
		pairs = append(pairs, headerPair{key: k, value: v})
	}
	if len(pairs) == 0 {
		return base
	}
	return &headerRoundTripper{
		base:    base,
		headers: pairs,
	}
}

func (rt *headerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	for _, kv := range rt.headers {
		req.Header.Set(kv.key, kv.value)
	}
	return rt.base.RoundTrip(req)
}
