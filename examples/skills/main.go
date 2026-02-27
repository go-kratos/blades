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

//go:embed skills
var skillFS embed.FS

func main() {
	model := openai.NewModel(os.Getenv("OPENAI_MODEL"), openai.Config{
		BaseURL: os.Getenv("OPENAI_BASE_URL"),
		APIKey:  os.Getenv("OPENAI_API_KEY"),
	})

	// A skill declared in code, similar to the Python sample's greeting skill.
	greetingSkill := &skills.Skill{
		Frontmatter: skills.Frontmatter{
			Name:        "greeting-skill",
			Description: "A friendly greeting skill that can say hello to a specific person.",
		},
		Instructions: "Step 1: Read 'references/hello_world.txt'.\nStep 2: Return a greeting based on the reference.",
		Resources: skills.Resources{
			References: map[string]string{
				"hello_world.txt": "Hello! Glad to have you here.",
				"example.md":      "This is an example reference.",
			},
		},
	}

	// Load the weather skill from embedded files.
	weatherSkills, err := skills.NewFromEmbed(skillFS)
	if err != nil {
		log.Fatal(err)
	}
	allSkills := append([]*skills.Skill{greetingSkill}, weatherSkills...)

	agent, err := blades.NewAgent(
		"SkillUserAgent",
		blades.WithModel(model),
		blades.WithInstruction("Use skills when they are relevant to the task."),
		blades.WithSkills(allSkills...),
	)
	if err != nil {
		log.Fatal(err)
	}

	runner := blades.NewRunner(agent)
	output, err := runner.Run(context.Background(), blades.UserMessage("What's the weather in San Francisco, and what's the humidity?"))
	if err != nil {
		log.Fatal(err)
	}
	log.Println(output.Text())
}
