package blades

import "github.com/go-kratos/blades/rag"

// Document 表示检索得到的文档或片段，包含内容、评分及自定义元数据。
type Document = rag.Document

// Indexer 负责向底层存储中添加或删除文档，以便后续检索使用。
type Indexer = rag.Indexer

// RetrieveOptions 包含检索时的可选参数。
type RetrieveOptions = rag.RetrieveOptions

// RetrieveOption 是用于配置检索选项的函数类型。
type RetrieveOption = rag.RetrieveOption

// Retriever 接口负责根据请求检索相关文档。
type Retriever = rag.Retriever

// Reranker 接口负责对初检索结果进行重排序，提升相关性。
type Reranker = rag.Reranker

var (
	// WithTopK 设置返回的最大文档数量。
	WithTopK = rag.WithTopK
	// WithConversationID 设置会话 ID。
	WithConversationID = rag.WithConversationID
	// WithFilters 设置过滤条件。
	WithFilters = rag.WithFilters
	// WithFilter 添加单个过滤条件。
	WithFilter = rag.WithFilter
)
