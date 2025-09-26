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

	provider := openai.NewImageProvider()

	req := &blades.ModelRequest{
		Model: "gpt-image-1",
		Messages: []*blades.Message{
			blades.UserMessage("A watercolor illustration of a mountain cabin at sunrise"),
		},
	}

	res, err := provider.Generate(
		ctx,
		req,
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
	for _, msg := range res.Messages {
		for _, part := range msg.Parts {
			switch img := part.(type) {
			case blades.DataPart:
				saved++
				name := fmt.Sprintf("image-%d.%s", saved, img.MimeType.Format())
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
}
