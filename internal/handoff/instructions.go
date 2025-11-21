package handoff

import (
	"bytes"
	"html/template"

	"github.com/go-kratos/blades"
)

const handoffInstructionTemplate = `You have access to the following agents:
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

var handoffToAgentPromptTmpl = template.Must(template.New("handoff_to_agent_prompt").Parse(handoffInstructionTemplate))

// BuildInstruction builds the instruction for handing off to another agent.
func BuildInstruction(targets []blades.Agent) (string, error) {
	var buf bytes.Buffer
	if err := handoffToAgentPromptTmpl.Execute(&buf, map[string]any{
		"Targets": targets,
	}); err != nil {
		return "", err
	}
	return buf.String(), nil
}
