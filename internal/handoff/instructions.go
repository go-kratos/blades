package handoff

import (
	"bytes"
	"html/template"

	"github.com/go-kratos/blades"
)

const transferInstructionTemplate = `You have a list of other agents to transfer to:
{{range .Targets}}
Agent name: {{.Name}}
Agent description: {{.Description}}
{{end}}
If you are the best to answer the question according to your description, you
can answer it.
If another agent is better for answering the question according to its
description, call 'handoff_to_agent' function to transfer the
question to that agent. When transferring, do not generate any text other than
the function call.`

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
