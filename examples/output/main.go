package main

import (
	"context"
	"log"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/contrib/openai"
)

// ActorsFilms represents an actor and their associated films.
type ActorsFilms struct {
	Actor  string   `json:"actor"`
	Movies []string `json:"movies"`
}

func main() {
	agent := blades.NewAgent(
		"filmography",
		blades.WithModel("gpt-5"),
		blades.WithProvider(openai.NewChatProvider()),
	)
	prompt := blades.NewPrompt(
		blades.UserMessage("Generate the filmography of 5 movies for Tom Hanks"),
	)
	converter := blades.NewOutputConverter[ActorsFilms](agent)
	actorsFilms, err := converter.Run(context.Background(), prompt)
	if err != nil {
		log.Fatal(err)
	}
	log.Println(actorsFilms)
}
