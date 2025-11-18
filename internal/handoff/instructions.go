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
- Determine whether you are the most appropriate agent to answer the user's question based on your own description.
- If another agent is clearly better suited to handle the user's request, you must transfer the query by calling the "handoff_to_agent" function.
- If no other agent is more suitable, respond to the user directly as a helpful assistant, providing clear, detailed, and accurate information.

Important rules:
- When transferring a query, output only the function call, and nothing else.
- Do not include explanations, reasoning, or any additional text outside of the function call.`

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
