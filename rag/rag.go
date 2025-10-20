package rag

import "context"

// Document 表示检索得到的文档或片段，包含内容、评分及自定义元数据。
type Document struct {
	ID        string
	Content   string
	Score     float64
	Metadata  map[string]any
	Embedding []float64 // 向量表示
}

// Indexer 负责向底层存储中添加或删除文档，以便后续检索使用。
type Indexer interface {
	Add(ctx context.Context, docs []Document) error
	Delete(ctx context.Context, docIDs []string) error
}

// RetrieveOptions 包含检索时的可选参数。
type RetrieveOptions struct {
	TopK           int
	ConversationID string
	Filters        map[string]string
}

// RetrieveOption 是用于配置检索选项的函数类型。
type RetrieveOption func(*RetrieveOptions)

// WithTopK 设置返回的最大文档数量。
func WithTopK(topK int) RetrieveOption {
	return func(o *RetrieveOptions) {
		o.TopK = topK
	}
}

// WithConversationID 设置会话 ID。
func WithConversationID(conversationID string) RetrieveOption {
	return func(o *RetrieveOptions) {
		o.ConversationID = conversationID
	}
}

// WithFilters 设置过滤条件。
func WithFilters(filters map[string]string) RetrieveOption {
	return func(o *RetrieveOptions) {
		o.Filters = filters
	}
}

// WithFilter 添加单个过滤条件。
func WithFilter(key, value string) RetrieveOption {
	return func(o *RetrieveOptions) {
		if o.Filters == nil {
			o.Filters = make(map[string]string)
		}
		o.Filters[key] = value
	}
}

// Retriever 接口负责根据请求检索相关文档。
type Retriever interface {
	Retrieve(ctx context.Context, query string, opts ...RetrieveOption) ([]Document, error)
}

// Reranker 接口负责对初检索结果进行重排序，提升相关性。
type Reranker interface {
	Rerank(ctx context.Context, query string, docs []Document) ([]Document, error)
}
