package main

import (
	"context"
	"log"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/contrib/openai"
	"github.com/google/jsonschema-go/jsonschema"
)

// ActorsFilms represents an actor and their associated films.
type ActorsFilms struct {
	Actor  string   `json:"actor" jsonschema:"name of the actor"`
	Movies []string `json:"movies" jsonschema:"list of movies"`
}

func main() {
	schema, err := jsonschema.For[ActorsFilms](nil)
	if err != nil {
		panic(err)
	}
	agent := blades.NewAgent(
		"filmography",
		blades.WithModel("gpt-5"),
		blades.WithProvider(openai.NewChatProvider()),
		blades.WithOutputSchema(schema),
	)
	input := blades.UserMessage("Generate the filmography of 5 movies for Tom Hanks")
	runner := blades.NewRunner(agent)
	actorsFilms, err := runner.Run(context.Background(), input)
	if err != nil {
		log.Fatal(err)
	}
	log.Println(actorsFilms)
}
