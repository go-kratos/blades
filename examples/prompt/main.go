package main

import (
	"context"
	"fmt"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/contrib/openai"
)

func main() {
	// directly define prompt, asking a question
	pb := blades.NewAgent(
		"prompt-basic",
		blades.WithModel("gpt-5"),
		blades.WithProvider(openai.NewChatProvider()),
	)

	basic := blades.NewPrompt(
		// UserMessage represents a message from the user.
		blades.UserMessage("What is the capital of France?"),
		// SystemMessage represents a message from the system that provides context or instructions to the model.
		blades.SystemMessage("You are a helpful assistant that provides detailed and accurate information."),
		// AssistantMessage represents a message from the assistant (the model) in the conversation.
		// It can be used to provide context or previous responses in a multi-turn conversation.
		// blades.AssistantMessage("The capital of France is Paris."),
	)

	msg, err := pb.Run(context.Background(), basic)
	if err != nil {
		panic(err)
	}
	fmt.Printf("Q: What is the capital of France?\nA: %s\n", msg.Text())

	// Using instructions to define the prompt, asking to verify the correctness of a mathematical formula
	pi := blades.NewAgent(
		"prompt-instructions",
		blades.WithModel("gpt-5"),
		blades.WithProvider(openai.NewChatProvider()),
		// WithInstructions sets the instructions for the agent.
		blades.WithInstructions(
			`You are a math teacher, mainly responsible for checking whether the arithmetic formulas submitted by users are correct.
    Please return them directly as right or wrong.`),
	)
	prompt := blades.NewPrompt(
		blades.UserMessage("1 + 1 = 3"),
	)
	msg, err = pi.Run(context.Background(), prompt)
	if err != nil {
		panic(err)
	}
	fmt.Printf("Q: 1 + 1 = 3\nA: %s\n", msg.Text())

	// Using a template to define the prompt, asking for a summary of a topic
	// template use text/template syntax
	tmp := map[string]any{
		"topic":  "little rabbit",
		"length": 100,
	}

	pt := blades.NewAgent(
		"prompt-template",
		blades.WithModel("gpt-5"),
		blades.WithProvider(openai.NewChatProvider()),
	)
	promptTemplate, err := blades.NewPromptTemplate().
		User("Please write a fairy tale about {{.topic}} for children, within {{.length}} words", tmp).Build()
	if err != nil {
		panic(err)
	}

	msg, err = pt.Run(context.Background(), promptTemplate)
	if err != nil {
		panic(err)
	}
	fmt.Printf("Q: Please write a fairy tale about little rabbit for children\nA: %s\n", msg.Text())
}
