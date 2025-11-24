package blades

const (
	// ActionInterrupted is the action name for an interrupted action.
	ActionInterrupted = "interrupted"
	// ActionHandoffToAgent is the action name for handing off to a sub-agent.
	ActionHandoffToAgent = "handoff_to_agent"
)

// Interrupted checks if the action map indicates an interrupted action.
func Interrupted(actions map[string]any) bool {
	interrupted, _ := actions[ActionInterrupted].(bool)
	return interrupted
}
