# RAG (Retrieval-Augmented Generation)

`rag/` 现仅包含核心接口和类型别名，所有实现均位于应用或示例中。开发者可以在自己的业务代码里实现 `rag.Indexer`、`rag.Retriever`、`rag.Reranker` 等接口，也可以参考 `examples/rag/` 的演示示例。

## 开始使用

```go
// rag.Document、rag.Indexer 等接口在 rag/rag.go 中导出，可直接在应用中实现。
type MyStore struct{}

func (s *MyStore) Add(ctx context.Context, docs []rag.Document) error    { /* ... */ }
func (s *MyStore) Delete(ctx context.Context, ids []string) error         { /* ... */ }
func (s *MyStore) Retrieve(ctx context.Context, query string, opts ...rag.RetrieveOption) ([]rag.Document, error) {
    /* ... */
}
```

想要快速体验，可以运行 `examples/rag/` 下的示例来查看完整的流水线编排方式，该示例展示了如何利用第三方库实现分块、检索和重排。

## 贡献指南

- 欢迎提交新的示例或第三方集成，帮助社区演示不同的 RAG 策略。
- 如需在仓库中新增通用组件，请先在 issue 中讨论需求及维护计划。
