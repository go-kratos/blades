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
		blades.WithInstructions("Please summarize {{.topic}} in three key points."),
	)
	runner := blades.NewRunner(agent)
	userTemplate := "Respond concisely and accurately for a {{.audience}} audience."
	// Set up HTTP handler
	mux := http.NewServeMux()
	mux.HandleFunc("/generate", func(w http.ResponseWriter, r *http.Request) {
		params := make(map[string]any)
		if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		input, err := blades.NewTemplateMessage(blades.RoleUser, userTemplate, params)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if stream, _ := params["stream"].(bool); stream {
			w.Header().Set("Content-Type", "text/event-stream")
			stream := runner.RunStream(r.Context(), input)
			for m, err := range stream {
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				json.NewEncoder(w).Encode(m)
				w.(http.Flusher).Flush() // Flush the response writer to send data immediately
			}
		} else {
			w.Header().Set("Content-Type", "application/json")
			output, err := runner.Run(r.Context(), input)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			json.NewEncoder(w).Encode(output)
		}
	})
	// Start HTTP server
	http.ListenAndServe(":8000", mux)
}
