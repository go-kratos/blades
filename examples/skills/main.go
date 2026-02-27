package main

import (
	"context"
	"embed"
	"log"
	"os"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/contrib/openai"
	"github.com/go-kratos/blades/skills"
)

//go:embed example-skill/*
var skillFS embed.FS

func main() {
	model := openai.NewModel(os.Getenv("OPENAI_MODEL"), openai.Config{
		BaseURL: os.Getenv("OPENAI_BASE_URL"),
		APIKey:  os.Getenv("OPENAI_API_KEY"),
	})

	// Load skills from embedded files.
	skillList, err := skills.NewFromEmbed(skillFS)
	if err != nil {
		log.Fatal(err)
	}

	agent, err := blades.NewAgent(
		"SkillsAgent",
		blades.WithModel(model),
		blades.WithInstruction("Use skills when they are relevant to the task."),
		blades.WithSkills(skillList...),
	)
	if err != nil {
		log.Fatal(err)
	}

	runner := blades.NewRunner(agent)
	output, err := runner.Run(context.Background(), blades.UserMessage("Review this document against our style guide."))
	if err != nil {
		log.Fatal(err)
	}
	log.Println(output.Text())
}
