package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/contrib/openai"
)

func main() {
	model := openai.NewModel(os.Getenv("OPENAI_MODEL"), openai.Config{
		APIKey: os.Getenv("OPENAI_API_KEY"),
	})
	agent, err := blades.NewAgent(
		"Server Agent",
		blades.WithModel(model),
		blades.WithInstruction("You are a helpful assistant that provides detailed and accurate information."),
	)
	if err != nil {
		log.Fatal(err)
	}
	// Set up HTTP handler
	mux := http.NewServeMux()
	mux.HandleFunc("/streaming", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		runner := blades.NewRunner(agent)
		input := blades.UserMessage(r.FormValue("input"))
		for output, err := range runner.RunStream(r.Context(), input) {
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			if err := json.NewEncoder(w).Encode(output); err != nil {
				return
			}
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
	})
	// Start HTTP server
	http.ListenAndServe(":8000", mux)
}
