package main

import (
	"encoding/json"
	"net/http"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/contrib/openai"
)

func main() {
	agent := blades.NewAgent(
		"Server Agent",
		blades.WithModel("gpt-5"),
		blades.WithProvider(openai.NewChatProvider()),
	)
	// Define templates and params
	systemTemplate := "Please summarize {{.topic}} in three key points."
	userTemplate := "Respond concisely and accurately for a {{.audience}} audience."
	// Set up HTTP handler
	mux := http.NewServeMux()
	mux.HandleFunc("/generate", func(w http.ResponseWriter, r *http.Request) {
		input := make(map[string]any)
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		prompt, err := blades.NewPromptTemplate().
			System(systemTemplate, input).
			User(userTemplate, input).
			Build()
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if stream, _ := input["stream"].(bool); stream {
			w.Header().Set("Content-Type", "text/event-stream")
			stream, err := agent.RunStream(r.Context(), prompt)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			defer stream.Close()
			for stream.Next() {
				chunk, err := stream.Current()
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				json.NewEncoder(w).Encode(chunk)
				w.(http.Flusher).Flush() // Flush the response writer to send data immediately
			}
		} else {
			w.Header().Set("Content-Type", "application/json")
			result, err := agent.Run(r.Context(), prompt)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			json.NewEncoder(w).Encode(result)
		}
	})
	// Start HTTP server
	http.ListenAndServe(":8000", mux)
}
