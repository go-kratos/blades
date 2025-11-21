package planner

import (
	"context"
	"strings"

	"github.com/go-kratos/blades"
)

const (
	tagPlanning    = "/*PLANNING*/"
	tagReplanning  = "/*REPLANNING*/"
	tagReasoning   = "/*REASONING*/"
	tagAction      = "/*ACTION*/"
	tagFinalAnswer = "/*FINAL_ANSWER*/"

	highLevelPreamble = `When answering the question, try to leverage the available tools to gather the information instead of your memorized knowledge.

Follow this process when answering the question: (1) first come up with a plan in natural language text format; (2) Then use tools to execute the plan and provide reasoning between tool code snippets to make a summary of current state and next step. Tool code snippets and reasoning should be interleaved with each other. (3) In the end, return one final answer.

Follow this format when answering the question: (1) The planning part should be under ` + tagPlanning + `. (2) The tool code snippets should be under ` + tagAction + `, and the reasoning parts should be under ` + tagReasoning + `. (3) The final answer part should be under ` + tagFinalAnswer + `.`

	planningPreamble = `Below are the requirements for the planning:

The plan is made to answer the user query if following the plan.The plan is coherent and covers all aspects of information from user query, and only involves the tools that are accessible by the agent.The plan contains the decomposed steps as a numbered list where each step should use one or multiple available tools.
By reading the plan, you can intuitively know which tools to trigger or what actions to take.If the initial plan cannot be successfully executed, you should learn from previous execution results and revise your plan. The revised plan should be be under ` + tagReplanning + `. Then use tools to follow the new plan.`

	reasoningPreamble = `Below are the requirements for the reasoning:

The reasoning makes a summary of the current trajectory based on the user query and tool outputs. Based on the tool outputs and plan, the reasoning also comes up with instructions to the next steps, making the trajectory closer to the final answer.`

	actionPreamble = `Below are the requirements for the action:

Explicitly state your next action in the first person ('I will...').
Execute your action using necessary tools and provide a concise summary of the outcome.`

	toolCodePreamble = `Below are the requirements for the tool code:

**Custom Tools:** The available tools are described in the context and can be directly used.
- Code must be valid self-contained Python snippets with no imports and no references to tools or Python libraries that are not in the context.
- You cannot use any parameters or fields that are not explicitly defined in the APIs in the context.
- The code snippets should be readable, efficient, and directly relevant to the user query and reasoning steps.
- When using the tools, you should use the library name together with the function name, e.g., vertex_search.search().
- If Python libraries are not provided in the context, NEVER write your own code other than the function calls using the provided tools.`

	finalAnswerPreamble = `Below are the requirements for the final answer:

The final answer should be precise and follow query formatting requirements. Some queries may not be answerable with the available tools and information. In those cases, inform the user why you cannot process their query and ask for more information.`

	userInputPreamble = `VERY IMPORTANT instruction that you MUST follow in addition to the above instructions:

You should ask for clarification if you need more information to answer the question.
You should prefer using the information available in the context instead of repeated tool use.`
)

// ReactOption configures the ReAct planner.
type ReactOption func(*reactPlanner)

// DisableReactVerbose disables detailed planning process.
func DisableReactVerbose() ReactOption {
	return func(p *reactPlanner) {
		p.verbose = false
	}
}

// reactPlanner implements the blades.Planner interface for ReAct pattern.
type reactPlanner struct {
	// verbose enables detailed planning process.
	verbose bool
}

// NewReactPlanner creates a new ReAct planner instance.
func NewReactPlanner(opts ...ReactOption) blades.Planner {
	p := &reactPlanner{
		verbose: true,
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// BuildInstruction builds the instruction for the ReAct planner.
func (p *reactPlanner) BuildInstruction(ctx context.Context) string {
	s := strings.Join([]string{
		highLevelPreamble,
		planningPreamble,
		actionPreamble,
		reasoningPreamble,
		finalAnswerPreamble,
		toolCodePreamble,
		userInputPreamble,
	}, "\n\n")
	return s
}

// ProcessMessage processes the message from the agent.
func (p *reactPlanner) ProcessMessage(ctx context.Context, message *blades.Message) *blades.Message {
	if message == nil {
		return message
	}
	response := *message
	if p.verbose {
		return &response
	}
	response.Parts = make([]blades.Part, 0, len(message.Parts))
	for _, part := range message.Parts {
		switch v := part.(type) {
		case blades.ToolPart:
			if v.Name == "" {
				continue
			}
		case blades.TextPart:
			if v.Text == "" {
				continue
			}
			if strings.Contains(v.Text, tagFinalAnswer) {
				finalAnswer := getFinalAnswer(v.Text)
				response.Parts = append(response.Parts, blades.TextPart{Text: finalAnswer})
				continue
			}
		default:
		}
		response.Parts = append(response.Parts, part)
	}
	return &response
}

func getFinalAnswer(text string) string {
	index := strings.LastIndex(text, tagFinalAnswer)
	if index == -1 {
		return ""
	}
	return text[index+len(tagFinalAnswer):]
}
