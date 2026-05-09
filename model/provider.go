package model

import (
	"context"
	"iter"
)

// Provider is the interface for LLM model backends.
type Provider interface {
	Name() string
	Generate(ctx context.Context, req *Request) (*Response, error)
	Stream(ctx context.Context, req *Request) iter.Seq2[*Chunk, error]
}

// EmbeddingRequest is the input for embedding generation.
type EmbeddingRequest struct {
	Model string
	Input []string
}

// EmbeddingResponse is the output of embedding generation.
type EmbeddingResponse struct {
	Embeddings [][]float64
	Usage      Usage
}

// EmbeddingProvider generates vector embeddings.
type EmbeddingProvider interface {
	Name() string
	Embed(ctx context.Context, req *EmbeddingRequest) (*EmbeddingResponse, error)
}
