package blades

// MaxIterations sets the maximum number of iterations for the model.
func MaxIterations(n int) ModelOption {
	return func(o *ModelOptions) {
		o.MaxIterations = n
	}
}

// MaxOutputTokens sets the maximum number of tokens to generate in the response.
func MaxOutputTokens(n int64) ModelOption {
	return func(o *ModelOptions) {
		o.MaxOutputTokens = n
	}
}

// TopP sets the nucleus sampling parameter.
func TopP(p float64) ModelOption {
	return func(o *ModelOptions) {
		o.TopP = p
	}
}

// Temperature sets the sampling temperature to use, between 0.0 and 1.0.
func Temperature(t float64) ModelOption {
	return func(o *ModelOptions) {
		o.Temperature = t
	}
}

// ReasoningEffort sets the level of reasoning effort for the model.
func ReasoningEffort(effort string) ModelOption {
	return func(o *ModelOptions) {
		o.ReasoningEffort = effort
	}
}
