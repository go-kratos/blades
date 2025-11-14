package main

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/contrib/openai"
)

func main() {
	model := openai.NewModel("gpt-5")
	agent, err := blades.NewAgent(
		"Server Agent",
		blades.WithModel(model),
		blades.WithInstructions("You are a helpful assistant that provides detailed and accurate information."),
	)
	if err != nil {
		log.Fatal(err)
	}
	// Set up HTTP handler
	mux := http.NewServeMux()
	mux.HandleFunc("/generate", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		runner := blades.NewRunner(agent)
		input := blades.UserMessage(r.FormValue("input"))
		output, err := runner.Run(r.Context(), input)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(output)
	})
	// Start HTTP server
	http.ListenAndServe(":8000", mux)
}
