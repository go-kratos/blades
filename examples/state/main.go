package main

import (
	"context"
	"log"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/contrib/openai"
	"github.com/go-kratos/blades/flow"
)

func main() {
	provider := openai.NewChatProvider()
	codeWriterAgent := blades.NewAgent(
		"CodeWriterAgent",
		blades.WithModel("gpt-5"),
		blades.WithProvider(provider),
		blades.WithInstructions(`You are a Python Code Generator.
Based *only* on the user's request, write Python code that fulfills the requirement.
Output *only* the complete Python code block, enclosed in triple backticks ("python ... "). 
Do not add any other text before or after the code block.`),
		blades.WithDescription("Writes initial Python code based on a specification."),
		blades.WithOutputKey("generated_code"),
	)
	codeReviewerAgent := blades.NewAgent(
		"CodeReviewerAgent",
		blades.WithModel("gpt-5"),
		blades.WithProvider(provider),
		blades.WithInstructions(`You are an expert Python Code Reviewer. 
    Your task is to provide constructive feedback on the provided code.

    **Code to Review:**
	{{.generated_code}}

**Review Criteria:**
1.  **Correctness:** Does the code work as intended? Are there logic errors?
2.  **Readability:** Is the code clear and easy to understand? Follows PEP 8 style guidelines?
3.  **Efficiency:** Is the code reasonably efficient? Any obvious performance bottlenecks?
4.  **Edge Cases:** Does the code handle potential edge cases or invalid inputs gracefully?
5.  **Best Practices:** Does the code follow common Python best practices?

**Output:**
Provide your feedback as a concise, bulleted list. Focus on the most important points for improvement.
If the code is excellent and requires no changes, simply state: "No major issues found."
Output *only* the review comments or the "No major issues" statement.`),
		blades.WithDescription("Reviews code and provides feedback."),
		blades.WithOutputKey("review_comments"),
	)
	codeRefactorerAgent := blades.NewAgent(
		"CodeRefactorerAgent",
		blades.WithModel("gpt-5"),
		blades.WithProvider(provider),
		blades.WithInstructions(`You are a Python Code Refactoring AI.
Your goal is to improve the given Python code based on the provided review comments.

  **Original Code:**
  {{.generated_code}}

  **Review Comments:**
  {{.review_comments}}

**Task:**
Carefully apply the suggestions from the review comments to refactor the original code.
If the review comments state "No major issues found," return the original code unchanged.
Ensure the final code is complete, functional, and includes necessary imports and docstrings.

**Output:**
Output *only* the final, refactored Python code block, enclosed in triple backticks ("python ... "). 
Do not add any other text before or after the code block.`),
		blades.WithDescription("Refactors code based on review comments."),
	)
	seq := flow.NewSequential(codeWriterAgent, codeReviewerAgent, codeRefactorerAgent)
	// Run the sequence with an initial user prompt
	prompt := blades.NewPrompt(
		blades.UserMessage("Write a Python function that takes a list of integers and returns a new list containing only the even integers from the original list."),
	)
	session := blades.NewSession()
	ctx := blades.NewSessionContext(context.Background(), session)
	result, err := seq.Run(ctx, prompt)
	if err != nil {
		log.Fatal(err)
	}
	// Log the entire session state
	session.State.Range(func(key string, value any) bool {
		log.Println(key, value)
		return true
	})
	log.Println(result.Text())
}
