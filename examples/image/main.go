package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/contrib/openai"
)

func main() {
	ctx := context.Background()

	agent := blades.NewAgent(
		"Image Agent",
		blades.WithModel("gpt-image-1"),
		blades.WithProvider(openai.NewImageProvider()),
	)

	prompt := blades.NewPrompt(
		blades.UserMessage("A watercolor illustration of a mountain cabin at sunrise"),
	)

	res, err := agent.Run(
		ctx,
		prompt,
		blades.ImageSize("1024x1024"),
		blades.ImageOutputFormat("png"),
	)
	if err != nil {
		log.Fatalf("generate image: %v", err)
	}

	outputDir := "generated"
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		log.Fatalf("create output dir: %v", err)
	}

	saved := 0
	for _, part := range res.Parts {
		switch img := part.(type) {
		case blades.DataPart:
			saved++
			name := fmt.Sprintf("image-%d.%s", saved, img.MIMEType.Format())
			path := filepath.Join(outputDir, name)
			if err := os.WriteFile(path, img.Bytes, 0o644); err != nil {
				log.Fatalf("write file %s: %v", path, err)
			}
			log.Printf("saved %s", path)
		case blades.FilePart:
			log.Printf("image url: %s", img.URI)
		}
	}
}
