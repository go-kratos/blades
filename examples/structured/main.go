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
	model := openai.NewModel("gpt-5")
	agent, err := blades.NewAgent(
		"filmography",
		blades.WithModel(model),
		blades.WithOutputSchema(schema),
	)
	if err != nil {
		log.Fatal(err)
	}

	input := blades.UserMessage("Generate the filmography of 5 movies for Tom Hanks")
	runner := blades.NewRunner(agent)
	actorsFilms, err := runner.Run(context.Background(), input)
	if err != nil {
		log.Fatal(err)
	}
	log.Println(actorsFilms)
}
