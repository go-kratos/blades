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

	m31, ok := Models[ModelM31Flash]
	if !ok {
		t.Fatalf("model %q missing from Models", ModelM31Flash)
	}
	if m31.ContextWindow != 1000000 {
		t.Errorf("M3.1 Flash ContextWindow = %d, want 1000000", m31.ContextWindow)
	}
	if got, want := len(m31.PricingTiers), 1; got != want {
		t.Fatalf("M3.1 Flash PricingTiers len = %d, want %d", got, want)
	}
	flashPrice := m31.PricingTiers[0]
	if flashPrice.MaxContextTokens != 1000000 || flashPrice.Pricing.Input != 0.15 || flashPrice.Pricing.Output != 0.6 || flashPrice.Pricing.CacheRead != 0.015 {
		t.Errorf("M3.1 Flash pricing tier = %+v, want max 1000000 input 0.15 output 0.6 cacheRead 0.015", flashPrice)
	}
	if got, want := len(m31.InputModalities), 3; got != want || !contains(m31.InputModalities, "text") || !contains(m31.InputModalities, "image") || !contains(m31.InputModalities, "video") {
		t.Errorf("M3.1 Flash InputModalities = %v, want text, image and video", m31.InputModalities)
	}
	if got, want := m31.Thinking, []string{ThinkingAdaptive}; !equalStrings(got, want) {
		t.Errorf("M3.1 Flash Thinking = %v, want %v", got, want)
	}

	m3, ok := Models[ModelM3]
	if !ok {
		t.Fatalf("model %q missing from Models", ModelM3)
	}
	if m3.ContextWindow != 1000000 {
		t.Errorf("M3 ContextWindow = %d, want 1000000", m3.ContextWindow)
	}
	if got, want := len(m3.PricingTiers), 2; got != want {
		t.Fatalf("M3 PricingTiers len = %d, want %d", got, want)
	}
	shortPrice := m3.PricingTiers[0]
	if shortPrice.MaxContextTokens != 512000 || shortPrice.Pricing.Input != 0.3 || shortPrice.Pricing.Output != 1.2 || shortPrice.Pricing.CacheRead != 0.06 {
		t.Errorf("M3 short-context pricing tier = %+v, want max 512000 input 0.3 output 1.2 cacheRead 0.06", shortPrice)
	}
	longPrice := m3.PricingTiers[1]
	if longPrice.MaxContextTokens != 1000000 || longPrice.Pricing.Input != 0.6 || longPrice.Pricing.Output != 2.4 || longPrice.Pricing.CacheRead != 0.12 {
		t.Errorf("M3 long-context pricing tier = %+v, want max 1000000 input 0.6 output 2.4 cacheRead 0.12", longPrice)
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
	if got := NewAnthropicModel(ModelM31Flash, Config{Region: RegionChina, APIKey: "test"}).Name(); got != ModelM31Flash {
		t.Errorf("NewAnthropicModel Name = %q, want %q", got, ModelM31Flash)
	}
}

func TestDefaultModelIsM31Flash(t *testing.T) {
	t.Parallel()
	if DefaultModel != ModelM31Flash {
		t.Errorf("DefaultModel = %q, want %q", DefaultModel, ModelM31Flash)
	}
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func contains(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}
