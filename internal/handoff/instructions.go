package handoff

import (
	"bytes"
	"html/template"

	"github.com/go-kratos/blades"
)

const transferInstructionTemplate = `You have access to the following agents:
{{range .Targets}}
Agent Name: {{.Name}}
Agent Description: {{.Description}}
{{end}}
Your task:
1. Determine whether YOU are the most suitable agent to answer the user's question based on your description.
2. If ANOTHER agent is more suitable based on their description, you MUST transfer the question by calling the 'handoff_to_agent' function.

Important rules:
- When transferring, output ONLY the function call and nothing else.
- Do not include explanations, reasoning, or extra text outside of the function call.`

var transferToAgentPromptTmpl = template.Must(template.New("transfer_to_agent_prompt").Parse(transferInstructionTemplate))

// BuildInstructions builds the instructions for transferring to another agent.
func BuildInstructions(targets []blades.Agent) (string, error) {
	var buf bytes.Buffer
	if err := transferToAgentPromptTmpl.Execute(&buf, map[string]any{
		"Targets": targets,
	}); err != nil {
		return "", err
	}
	return buf.String(), nil
}
