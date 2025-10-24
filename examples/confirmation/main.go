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
)

// confirmPrompt is a simple interactive confirmer that asks the user
// to approve the incoming prompt before allowing the agent to run.
func confirmPrompt(ctx context.Context, p *blades.Prompt) (bool, error) {
	preview := strings.TrimSpace(p.Latest().Text())
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
	agent := blades.NewAgent(
		"ConfirmAgent",
		blades.WithModel("gpt-5"),
		blades.WithInstructions("Answer clearly and concisely."),
		blades.WithProvider(openai.NewChatProvider()),
		blades.WithMiddleware(blades.Confirm(confirmPrompt)),
	)

	// Example user request
	prompt := blades.NewPrompt(
		blades.UserMessage("Summarize the key ideas of the Agile Manifesto in 3 bullet points."),
	)

	// Run the agent; if the confirmation is denied, handle gracefully.
	res, err := agent.Run(context.Background(), prompt)
	if err != nil {
		if errors.Is(err, blades.ErrConfirmationDenied) {
			log.Println("Confirmation denied. Aborting.")
			return
		}
		log.Fatal(err)
	}
	log.Println(res.Text())
}
