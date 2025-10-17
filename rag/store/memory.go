package store

import (
	"context"
	"sort"
	"sync"

	"github.com/go-kratos/blades/rag"
	"github.com/go-kratos/blades/rag/retrieval"
	"github.com/google/uuid"
)

// MemoryStore implements a minimal in-memory indexer and retriever for documents.
type MemoryStore struct {
	mu   sync.RWMutex
	docs map[string]rag.Document
	bm25 *retrieval.BM25Scorer
}

// NewMemoryStore creates an empty in-memory store for RAG experiments.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		docs: make(map[string]rag.Document),
		bm25: retrieval.NewBM25Scorer(),
	}
}

// Add stores or updates the provided documents.
func (s *MemoryStore) Add(_ context.Context, docs []rag.Document) error {
	if len(docs) == 0 {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, doc := range docs {
		if doc.ID == "" {
			doc.ID = uuid.NewString()
		}
		if doc.Metadata == nil {
			doc.Metadata = make(map[string]any)
		}
		s.docs[doc.ID] = doc
	}

	// 重建 BM25 索引
	allDocs := make([]rag.Document, 0, len(s.docs))
	for _, doc := range s.docs {
		allDocs = append(allDocs, doc)
	}
	s.bm25.Index(allDocs)

	return nil
}

// Delete removes the documents with the given IDs.
func (s *MemoryStore) Delete(_ context.Context, docIDs []string) error {
	if len(docIDs) == 0 {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, id := range docIDs {
		delete(s.docs, id)
	}

	// 重建 BM25 索引
	allDocs := make([]rag.Document, 0, len(s.docs))
	for _, doc := range s.docs {
		allDocs = append(allDocs, doc)
	}
	s.bm25.Index(allDocs)

	return nil
}

// Retrieve returns the top K documents ranked by BM25.
func (s *MemoryStore) Retrieve(_ context.Context, query string, opts ...rag.RetrieveOption) ([]rag.Document, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(s.docs) == 0 {
		return nil, nil
	}

	// Apply options
	options := rag.RetrieveOptions{}
	for _, opt := range opts {
		opt(&options)
	}

	tokens := retrieval.Tokenize(query)
	results := make([]rag.Document, 0, len(s.docs))

	for _, doc := range s.docs {
		if !MatchFilters(doc, options.Filters) {
			continue
		}

		scored := doc
		// 使用 BM25 算法计算分数
		scored.Score = s.bm25.Score(query, doc)

		// 如果有查询词但分数为0，跳过该文档
		if len(tokens) > 0 && scored.Score == 0 {
			continue
		}

		results = append(results, scored)
	}

	if len(results) == 0 {
		return nil, nil
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].Score == results[j].Score {
			return results[i].ID < results[j].ID
		}
		return results[i].Score > results[j].Score
	})

	topK := options.TopK
	if topK <= 0 || topK > len(results) {
		topK = len(results)
	}

	return results[:topK], nil
}
