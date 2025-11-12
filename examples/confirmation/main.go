package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/contrib/openai"
	"github.com/go-kratos/blades/middleware"
)

// confirmPrompt is a simple interactive confirmer that asks the user
// to approve the incoming prompt before allowing the agent to run.
func confirmPrompt(ctx context.Context, message *blades.Message) (bool, error) {
	preview := strings.TrimSpace(message.Text())
	fmt.Println("Request preview:")
	fmt.Println(preview)
	fmt.Print("Proceed? [y/N]: ")
	// Read user input from stdin
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return false, fmt.Errorf("failed to read input: %w", err)
	}
	line = strings.TrimSpace(strings.ToLower(line))
	return line == "y" || line == "yes", nil
}

func main() {
	// Create an agent and wrap it with the confirmation middleware.
	agent, err := blades.NewAgent(
		"ConfirmAgent",
		blades.WithModel("gpt-5"),
		blades.WithInstructions("Answer clearly and concisely."),
		blades.WithProvider(openai.NewChatProvider()),
		blades.WithMiddleware(middleware.Confirm(confirmPrompt)),
	)
	if err != nil {
		log.Fatal(err)
	}
	// Example user request
	input := blades.UserMessage("Summarize the key ideas of the Agile Manifesto in 3 bullet points.")
	// Run the agent; if the confirmation is denied, handle gracefully.
	runner := blades.NewRunner(agent)
	output, err := runner.Run(context.Background(), input)
	if err != nil {
		if errors.Is(err, middleware.ErrConfirmDenied) {
			log.Println("Confirmation denied. Aborting.")
			return
		}
		log.Fatal(err)
	}
	log.Println(output.Text())
}
