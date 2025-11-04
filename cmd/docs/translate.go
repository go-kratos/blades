package main

import (
	"context"
	"os"
	"path/filepath"

	"github.com/openai/openai-go/v2/option"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/contrib/openai"
)

func translate(from string) error {
	content, err := os.ReadFile(from)
	if err != nil {
		return err
	}
	provider := openai.NewChatProvider(
		openai.WithChatOptions(
			option.WithBaseURL(baseURL),
			option.WithAPIKey(apiKey),
		),
	)
	agent := blades.NewAgent(
		"Document translator",
		blades.WithModel(model),
		blades.WithProvider(provider),
		blades.WithInstructions(`You are a professional technical translator.
	Please translate the following Markdown document into **{{.target_language}}**.
	Follow these strict rules:
	1. **Preserve all Markdown formatting**, including headings, bold/italic text, lists, quotes, tables, code blocks, links, and images.
	2. **Do not translate code**, filenames, paths, variable names, commands, URLs, or HTML tags.
	3. **Keep technical terms consistent** (e.g., API, SDK, Server, Client — keep them untranslated when appropriate).
	4. The translation should be **natural, accurate, and professional**.
	5. **Keep the same paragraph structure and line breaks** as in the original.
	6. For mixed-language content, maintain logical consistency.
	7. Output **only the translated Markdown document** — do not add explanations, comments, or extra text.`),
	)
	prompt := blades.NewPrompt(
		blades.UserMessage(string(content)),
	)
	session := blades.NewSession("translate", map[string]any{
		"target_language": to,
	})
	ctx := blades.NewSessionContext(context.Background(), session)
	result, err := agent.Run(ctx, prompt)
	if err != nil {
		return err
	}
	dir, _ := filepath.Split(translateOutput(from, output))
	if _, err = os.Stat(dir); os.IsNotExist(err) && dir != "" {
		if err = os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	return os.WriteFile(translateOutput(from, output), []byte(result.Text()), 0644)
}

func translateOutput(from, output string) string {
	base := filepath.Base(from)
	return filepath.Join(output, base)
}
