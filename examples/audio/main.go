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
	agent := blades.NewAgent(
		"Audio Agent",
		blades.WithModel("gpt-4o-mini-tts"),
		blades.WithProvider(openai.NewAudioProvider()),
	)
	runner := blades.NewRunner(agent)
	output, err := runner.Run(
		context.Background(),
		blades.UserMessage("Welcome to the Blades audio demo!"),
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
	for n, part := range output.Parts {
		switch audio := part.(type) {
		case blades.DataPart:
			path := filepath.Join(outputDir, fmt.Sprintf("speech-%d.mp3", n))
			if err := os.WriteFile(path, audio.Bytes, 0o644); err != nil {
				log.Fatalf("write file %s: %v", path, err)
			}
			log.Printf("saved %s", path)
		case blades.FilePart:
			log.Printf("streamed audio url: %s", audio.URI)
		}
	}
}
