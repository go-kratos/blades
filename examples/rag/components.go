package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/go-kratos/blades/rag"
)

// SentenceChunker performs a naïve sentence-based split with a soft length limit.
type SentenceChunker struct {
	maxChars int
}

// NewSentenceChunker creates a chunker that tries to keep each chunk within maxChars.
func NewSentenceChunker(maxChars int) *SentenceChunker {
	if maxChars <= 0 {
		maxChars = 200
	}
	return &SentenceChunker{maxChars: maxChars}
}

// Split divides the text into chunks, attempting to preserve sentence boundaries.
func (c *SentenceChunker) Split(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}

	var (
		chunks []string
		buf    strings.Builder
		count  int
	)

	flush := func(force bool) {
		if buf.Len() == 0 {
			return
		}
		if force || buf.Len() >= c.maxChars {
			chunks = append(chunks, strings.TrimSpace(buf.String()))
			buf.Reset()
			count = 0
		}
	}

	for _, r := range text {
		buf.WriteRune(r)
		if r == '\n' {
			flush(true)
			continue
		}

		count += runeLength(r)
		if isSentenceBoundary(r) {
			flush(false)
			continue
		}

		if count >= c.maxChars {
			flush(true)
		}
	}

	flush(true)
	return chunks
}

// SimpleMemoryStore keeps documents in memory and scores them with a Jaccard similarity.
type SimpleMemoryStore struct {
	mu      sync.RWMutex
	docs    map[string]rag.Document
	counter int
}

// NewSimpleMemoryStore creates an in-memory implementation for demos.
func NewSimpleMemoryStore() *SimpleMemoryStore {
	return &SimpleMemoryStore{
		docs: make(map[string]rag.Document),
	}
}

// Add stores or updates the provided documents.
func (s *SimpleMemoryStore) Add(_ context.Context, docs []rag.Document) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range docs {
		doc := docs[i]
		id := strings.TrimSpace(doc.ID)
		if id == "" {
			s.counter++
			id = generateID(s.counter)
		}
		doc.ID = id
		if doc.Metadata == nil {
			doc.Metadata = make(map[string]any)
		}
		s.docs[doc.ID] = doc
	}

	return nil
}

// Delete removes documents by ID.
func (s *SimpleMemoryStore) Delete(_ context.Context, docIDs []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, id := range docIDs {
		delete(s.docs, id)
	}
	return nil
}

// Retrieve returns the best matching documents using a simple similarity score.
func (s *SimpleMemoryStore) Retrieve(_ context.Context, query string, opts ...rag.RetrieveOption) ([]rag.Document, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(s.docs) == 0 {
		return nil, nil
	}

	options := rag.RetrieveOptions{}
	for _, opt := range opts {
		opt(&options)
	}

	queryTokens := tokenize(query)
	var results []rag.Document

	for _, doc := range s.docs {
		if !matchFilters(doc, options.Filters) {
			continue
		}

		score := jaccardSimilarity(queryTokens, tokenize(doc.Content))
		if len(queryTokens) > 0 && score == 0 {
			continue
		}

		copied := doc
		copied.Score = score
		results = append(results, copied)
	}

	if len(results) == 0 {
		return nil, nil
	}

	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Score == results[j].Score {
			return results[i].ID < results[j].ID
		}
		return results[i].Score > results[j].Score
	})

	topK := options.TopK
	if topK <= 0 || topK > len(results) {
		topK = len(results)
	}

	return append([]rag.Document(nil), results[:topK]...), nil
}

// ScoreFunc allows injecting extra scoring logic during reranking.
type ScoreFunc func(ctx context.Context, query string, doc rag.Document) (float64, error)

// SimpleReranker re-orders documents by combining existing scores with token overlap.
type SimpleReranker struct {
	scorer ScoreFunc
}

// NewSimpleReranker constructs a reranker based on token overlap and optional custom scoring.
func NewSimpleReranker(scorer ScoreFunc) *SimpleReranker {
	return &SimpleReranker{scorer: scorer}
}

// Rerank reorders documents by combining previous scores with a secondary similarity.
func (r *SimpleReranker) Rerank(ctx context.Context, query string, docs []rag.Document) ([]rag.Document, error) {
	if len(docs) == 0 {
		return nil, nil
	}

	queryTokens := tokenize(query)
	scored := make([]rag.Document, len(docs))
	for i, doc := range docs {
		docCopy := doc
		overlap := jaccardSimilarity(queryTokens, tokenize(doc.Content))
		if r.scorer != nil {
			custom, err := r.scorer(ctx, query, doc)
			if err != nil {
				return nil, err
			}
			overlap += custom
		}
		docCopy.Score = doc.Score + overlap
		scored[i] = docCopy
	}

	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].Score == scored[j].Score {
			return scored[i].ID < scored[j].ID
		}
		return scored[i].Score > scored[j].Score
	})

	return scored, nil
}

func isSentenceBoundary(r rune) bool {
	switch r {
	case '.', '!', '?', '。', '！', '？', ';', '；':
		return true
	default:
		return false
	}
}

func runeLength(r rune) int {
	if r < utf8.RuneSelf {
		return 1
	}
	return 2
}

func tokenize(text string) []string {
	if text == "" {
		return nil
	}

	fields := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !(unicode.IsLetter(r) || unicode.IsDigit(r))
	})

	var tokens []string
	for _, field := range fields {
		if field != "" {
			tokens = append(tokens, field)
		}
	}
	return tokens
}

func jaccardSimilarity(a, b []string) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}

	setA := make(map[string]struct{}, len(a))
	for _, token := range a {
		setA[token] = struct{}{}
	}

	setB := make(map[string]struct{}, len(b))
	for _, token := range b {
		setB[token] = struct{}{}
	}

	intersection := 0
	for token := range setA {
		if _, ok := setB[token]; ok {
			intersection++
		}
	}

	if intersection == 0 {
		return 0
	}

	union := len(setA) + len(setB) - intersection
	return float64(intersection) / float64(union)
}

func matchFilters(doc rag.Document, filters map[string]string) bool {
	if len(filters) == 0 {
		return true
	}

	for key, expected := range filters {
		value, ok := doc.Metadata[key]
		if !ok {
			return false
		}
		str, ok := value.(string)
		if !ok || str != expected {
			return false
		}
	}

	return true
}

func generateID(counter int) string {
	return fmt.Sprintf("doc-%d", counter)
}
