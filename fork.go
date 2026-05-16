package blades

// Fork creates a new default LLM agent from an existing default LLM agent.
func Fork(agent Agent, opts ...AgentOption) (Agent, error) {
	base, ok := llmAgentFromAgent(agent)
	if !ok {
		return nil, ErrAgentNotForkable
	}
	fork := base.clone()
	fork.name = base.name + "-fork"
	for _, opt := range opts {
		opt(fork)
	}
	if fork.provider == nil {
		return nil, ErrModelProviderRequired
	}
	if err := fork.prepareContextCounting(); err != nil {
		return nil, err
	}
	return fork, nil
}

func llmAgentFromAgent(agent Agent) (*llmAgent, bool) {
	if wrapper, ok := agent.(interface{ unwrapAgent() Agent }); ok {
		agent = wrapper.unwrapAgent()
	}
	base, ok := agent.(*llmAgent)
	return base, ok
}
