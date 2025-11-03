package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/contrib/openai"
	"github.com/openai/openai-go/v2/option"
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
	)
	prompt, err := blades.NewPromptTemplate().
		System(`You are a professional technical translator.
Please translate the following Markdown document into **{{.target_language}}**.
Follow these strict rules:

1. **Preserve all Markdown formatting**, including headings, bold/italic text, lists, quotes, tables, code blocks, links, and images.
2. **Do not translate code**, filenames, paths, variable names, commands, URLs, or HTML tags.
3. **Keep technical terms consistent** (e.g., API, SDK, Server, Client — keep them untranslated when appropriate).
4. The translation should be **natural, accurate, and professional**.
5. **Keep the same paragraph structure and line breaks** as in the original.
6. For mixed-language content, maintain logical consistency.
7. Output **only the translated Markdown document** — do not add explanations, comments, or extra text.
`, map[string]any{"target_language": to}).User(string(content)).Build()
	if err != nil {
		return err
	}
	result, err := agent.Run(context.Background(), prompt)
	if err != nil {
		return err
	}
	if err := os.WriteFile(translateOutput(from, to), []byte(result.Text()), 0644); err != nil {
		return err
	}
	return nil
}

func translateOutput(from, to string) string {
	base := filepath.Base(from)
	dir := filepath.Dir(from)
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)
	parts := strings.Split(name, "_")
	if len(parts) > 1 {
		parts = parts[:len(parts)-1]
	}
	newName := strings.Join(parts, "_")
	if to != "en" {
		newName = newName + "_" + to
	}
	return filepath.Join(dir, newName+ext)
}
