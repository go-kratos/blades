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

	provider := openai.NewAudioProvider()

	req := &blades.ModelRequest{
		Model: "gpt-4o-mini-tts",
		Messages: []*blades.Message{
			blades.UserMessage("Welcome to the Blades audio demo!"),
		},
	}

	res, err := provider.Generate(
		ctx,
		req,
		blades.AudioVoice("alloy"),
		blades.AudioResponseFormat("mp3"),
	)
	if err != nil {
		log.Fatalf("generate audio: %v", err)
	}

	outputDir := "generated"
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		log.Fatalf("create output dir: %v", err)
	}

	saved := 0
	for _, msg := range res.Messages {
		for _, part := range msg.Parts {
			switch audio := part.(type) {
			case blades.DataPart:
				saved++
				ext := "bin"
				if format := msg.Metadata["response_format"]; format != "" {
					ext = format
				} else if mimeExt := audio.MimeType.Format(); mimeExt != "" {
					ext = mimeExt
				}
				path := filepath.Join(outputDir, fmt.Sprintf("speech-%d.%s", saved, ext))
				if err := os.WriteFile(path, audio.Bytes, 0o644); err != nil {
					log.Fatalf("write file %s: %v", path, err)
				}
				log.Printf("saved %s", path)
			case blades.FilePart:
				log.Printf("streamed audio url: %s", audio.URI)
			}
		}
	}
}
