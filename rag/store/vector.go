package store

import (
	"context"
	"sort"
	"sync"

	"github.com/go-kratos/blades/rag"
	"github.com/go-kratos/blades/rag/retrieval"
	"github.com/google/uuid"
)

// VectorStore 实现基于向量相似度的文档检索。
// 注意：文档必须在添加前预先生成好 Embedding 字段。
type VectorStore struct {
	mu   sync.RWMutex
	docs map[string]rag.Document
	bm25 *retrieval.BM25Scorer // 混合检索：BM25 + 向量
}

// NewVectorStore 创建一个向量存储。
func NewVectorStore() *VectorStore {
	return &VectorStore{
		docs: make(map[string]rag.Document),
		bm25: retrieval.NewBM25Scorer(),
	}
}

// Add 添加文档。文档必须预先包含 Embedding 字段。
func (s *VectorStore) Add(ctx context.Context, docs []rag.Document) error {
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

// Delete 删除文档。
func (s *VectorStore) Delete(ctx context.Context, docIDs []string) error {
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

// Retrieve 使用混合检索：向量相似度 + BM25。
// 查询向量需要在 req 中通过元数据传递，或者使用纯 BM25 检索。
func (s *VectorStore) Retrieve(ctx context.Context, query string, opts ...rag.RetrieveOption) ([]rag.Document, error) {
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

	results := make([]rag.Document, 0, len(s.docs))

	for _, doc := range s.docs {
		if !MatchFilters(doc, options.Filters) {
			continue
		}

		scored := doc

		// 如果文档有向量，尝试计算向量相似度
		// 调用方需要在外部准备查询向量并通过某种方式传递进来
		// 这里暂时只使用 BM25
		bm25Score := s.bm25.Score(query, doc)
		scored.Score = bm25Score

		results = append(results, scored)
	}

	if len(results) == 0 {
		return nil, nil
	}

	// 排序
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
