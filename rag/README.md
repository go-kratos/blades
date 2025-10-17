# RAG (Retrieval-Augmented Generation)

`rag/` 提供构建 RAG 流水线的基础组件：文档切分、检索评分、结果重排序以及示例流水线。核心接口定义在 `rag/rag.go`，与根模块中的类型保持一致，方便在应用侧直接引用。

## 组件概览

- **Chunking** (`rag/chunking/chunking.go`): 提供句子级与固定大小两种分块策略，处理多字节字符不会截断。
- **Retrieval** (`rag/retrieval/bm25.go`): 纯 Go 实现的 BM25 打分器及基础分词工具。
- **Stores** (`rag/store/*.go`): 内存实现的 `Indexer`/`Retriever`，已集成 BM25 排序；`VectorStore` 预留向量字段以便后续扩展混合检索。
- **Rerankers** (`rag/retrieval/reranker.go`): 交叉编码器重排、RRF 融合等策略，支持自定义打分函数。
- **示例** (`examples/rag/`): 使用 `flow.Graph` 组合分块、索引、检索、重排和 LLM 生成的完整演示。

## 快速上手

```go
store := store.NewMemoryStore()
chunker := chunking.NewSentenceChunker(150)

chunks := chunker.Split(longDocument)
docs := make([]rag.Document, len(chunks))
for i, text := range chunks {
    docs[i] = rag.Document{ID: fmt.Sprintf("doc-%d", i), Content: text}
}

if err := store.Add(ctx, docs); err != nil {
    log.Fatal(err)
}

results, err := store.Retrieve(ctx, "rainy commute tips", rag.WithTopK(3))
if err != nil {
    log.Fatal(err)
}
```

更多细节可参考 `examples/rag/main.go`，示例中演示了如何通过 `flow.Graph` 编排分块、索引、检索、重排以及调用 LLM 生成最终回答。

## 扩展方向

- 将 `rag/store/vector.go` 中的占位逻辑替换为真实向量相似度或外部向量数据库。
- 接入外部重排模型，在 `retrieval.NewCrossEncoderReranker` 中注入实际的评分函数。
- 在 `rag/chunking` 增加按结构化数据（如标题、段落）分块的策略。

## 测试

```bash
go test ./rag/... -race
```

现有测试覆盖了多字节分块、BM25 打分、内存存储检索等关键路径，建议在扩展组件时追加对应的 `*_test.go`。
