# Ollama Provider

Ollama `ModelProvider` implementation for Blades.

## Install

```bash
go get github.com/go-kratos/blades/contrib/ollama
```

## Features

- `Generate` non-streaming chat
- `NewStreaming` NDJSON streaming chat
- tool schema support (`ModelRequest.Tools`)
- structured output support (`ModelRequest.OutputSchema` -> `format`)
- image input support (`FilePart` / `DataPart` with image MIME type)
- configurable `BaseURL`, request headers, options and keep-alive

## Usage

```go
package main

import (
	"context"
	"log"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/contrib/ollama"
)

func main() {
	model := ollama.NewModel("llama3.2", ollama.Config{
		BaseURL: "http://127.0.0.1:11434", // optional
		Headers: map[string]string{
			"Authorization": "Bearer <token>",
		},
		Options: map[string]any{
			"temperature": 0.2,
		},
	})

	agent := blades.NewAgent(
		"assistant",
		blades.WithModel(model),
	)

	out, err := blades.NewRunner(agent).Run(context.Background(), blades.UserMessage("你好"))
	if err != nil {
		log.Fatal(err)
	}
	log.Println(out.Text())
}
```
