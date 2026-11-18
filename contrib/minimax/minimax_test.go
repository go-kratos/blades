package minimax

import "testing"

func TestEndpointsCoverBothRegions(t *testing.T) {
	t.Parallel()
	cases := []struct {
		region    Region
		openAI    string
		anthropic string
		docs      string
	}{
		{RegionGlobal, "https://api.minimax.io/v1", "https://api.minimax.io/anthropic", "https://platform.minimax.io/docs"},
		{RegionChina, "https://api.minimaxi.com/v1", "https://api.minimaxi.com/anthropic", "https://platform.minimaxi.com/docs"},
	}
	for _, tc := range cases {
		ep, ok := Endpoints[tc.region]
		if !ok {
			t.Fatalf("region %q missing from Endpoints", tc.region)
		}
		if ep.OpenAIBaseURL != tc.openAI {
			t.Errorf("region %q OpenAIBaseURL = %q, want %q", tc.region, ep.OpenAIBaseURL, tc.openAI)
		}
		if ep.AnthropicBaseURL != tc.anthropic {
			t.Errorf("region %q AnthropicBaseURL = %q, want %q", tc.region, ep.AnthropicBaseURL, tc.anthropic)
		}
		if ep.DocsRoot != tc.docs {
			t.Errorf("region %q DocsRoot = %q, want %q", tc.region, ep.DocsRoot, tc.docs)
		}
	}
}

func TestModelMetadata(t *testing.T) {
	t.Parallel()

	m3, ok := Models[ModelM3]
	if !ok {
		t.Fatalf("model %q missing from Models", ModelM3)
	}
	if m3.ContextWindow != 1000000 {
		t.Errorf("M3 ContextWindow = %d, want 1000000", m3.ContextWindow)
	}
	if m3.Pricing.Input != 0.6 || m3.Pricing.Output != 2.4 || m3.Pricing.CacheRead != 0.12 {
		t.Errorf("M3 pricing = %+v, want input 0.6 output 2.4 cacheRead 0.12", m3.Pricing)
	}
	if m3.Pricing.CacheWrite != nil {
		t.Errorf("M3 CacheWrite = %v, want nil", *m3.Pricing.CacheWrite)
	}
	if got, want := len(m3.InputModalities), 3; got != want {
		t.Errorf("M3 InputModalities len = %d, want %d", got, want)
	}
	if !contains(m3.InputModalities, "image") || !contains(m3.InputModalities, "video") {
		t.Errorf("M3 InputModalities = %v, want image and video", m3.InputModalities)
	}
	if !contains(m3.Thinking, ThinkingAdaptive) || !contains(m3.Thinking, ThinkingDisabled) {
		t.Errorf("M3 Thinking = %v, want adaptive and disabled", m3.Thinking)
	}

	m27, ok := Models[ModelM27]
	if !ok {
		t.Fatalf("model %q missing from Models", ModelM27)
	}
	if m27.ContextWindow != 204800 {
		t.Errorf("M2.7 ContextWindow = %d, want 204800", m27.ContextWindow)
	}
	if m27.Pricing.Input != 0.3 || m27.Pricing.Output != 1.2 || m27.Pricing.CacheRead != 0.06 {
		t.Errorf("M2.7 pricing = %+v, want input 0.3 output 1.2 cacheRead 0.06", m27.Pricing)
	}
	if m27.Pricing.CacheWrite == nil || *m27.Pricing.CacheWrite != 0.375 {
		t.Errorf("M2.7 CacheWrite = %v, want 0.375", m27.Pricing.CacheWrite)
	}
	if got, want := len(m27.InputModalities), 1; got != want || m27.InputModalities[0] != "text" {
		t.Errorf("M2.7 InputModalities = %v, want [text]", m27.InputModalities)
	}
	if got, want := len(m27.Thinking), 1; got != want || m27.Thinking[0] != ThinkingAlwaysOn {
		t.Errorf("M2.7 Thinking = %v, want [always_on]", m27.Thinking)
	}
}

func TestBaseURLResolution(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		config    Config
		openAI    string
		anthropic string
	}{
		{"default region is global", Config{}, "https://api.minimax.io/v1", "https://api.minimax.io/anthropic"},
		{"explicit global", Config{Region: RegionGlobal}, "https://api.minimax.io/v1", "https://api.minimax.io/anthropic"},
		{"china", Config{Region: RegionChina}, "https://api.minimaxi.com/v1", "https://api.minimaxi.com/anthropic"},
		{"unknown region falls back to global", Config{Region: Region("mars")}, "https://api.minimax.io/v1", "https://api.minimax.io/anthropic"},
		{"explicit base url overrides", Config{Region: RegionChina, BaseURL: "https://proxy.example/v1"}, "https://proxy.example/v1", "https://proxy.example/v1"},
	}
	for _, tc := range cases {
		if got := tc.config.openAIBaseURL(); got != tc.openAI {
			t.Errorf("%s: openAIBaseURL = %q, want %q", tc.name, got, tc.openAI)
		}
		if got := tc.config.anthropicBaseURL(); got != tc.anthropic {
			t.Errorf("%s: anthropicBaseURL = %q, want %q", tc.name, got, tc.anthropic)
		}
	}
}

func TestNewModelReportsName(t *testing.T) {
	t.Parallel()
	if got := NewModel(ModelM3, Config{APIKey: "test"}).Name(); got != ModelM3 {
		t.Errorf("NewModel Name = %q, want %q", got, ModelM3)
	}
	if got := NewAnthropicModel(ModelM27, Config{Region: RegionChina, APIKey: "test"}).Name(); got != ModelM27 {
		t.Errorf("NewAnthropicModel Name = %q, want %q", got, ModelM27)
	}
}

func TestDefaultModelIsM3(t *testing.T) {
	t.Parallel()
	if DefaultModel != ModelM3 {
		t.Errorf("DefaultModel = %q, want %q", DefaultModel, ModelM3)
	}
}

func contains(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}
