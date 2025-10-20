package main

import (
	"context"
	"fmt"
	"log"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/contrib/openai"
	"github.com/go-kratos/blades/flow"
	"github.com/go-kratos/blades/rag"
)

func main() {
	ctx := context.Background()

	// 1. 创建存储和组件（示例实现定义在本目录下）
	s := NewSimpleMemoryStore()

	// 2. 创建自定义评分函数用于重排序
	scorer := func(ctx context.Context, query string, doc rag.Document) (float64, error) {
		// 简单示例：基于内容长度的分数
		return float64(len(doc.Content)) / 100.0, nil
	}
	reranker := NewSimpleReranker(scorer)

	// 3. 创建 LLM Agent（使用 OpenAI）
	provider := openai.NewChatProvider()
	agent := blades.NewAgent(
		"rag-assistant",
		blades.WithProvider(provider),
		blades.WithModel("gpt-4o-mini"),
	)

	// 4. 创建各个节点
	chunkingNode := NewChunkingNode()
	indexingNode := NewIndexingNode(s)
	retrievalNode := NewRetrievalNode(s)
	rerankingNode := NewRerankingNode(reranker)
	generationNode := NewGenerationNode(agent)

	// 5. 定义状态转换处理器（节点间传递 RAGState）
	transitionHandler := func(ctx context.Context, transition flow.Transition, output *RAGState) (*RAGState, error) {
		log.Printf("[Transition] %s -> %s\n", transition.From, transition.To)
		return output, nil
	}

	// 6. 构建 Graph
	g := flow.NewGraph[*RAGState, *RAGState, blades.ModelOption]("rag-pipeline", transitionHandler)

	// 添加节点
	g.AddNode(chunkingNode)
	g.AddNode(indexingNode)
	g.AddNode(retrievalNode)
	g.AddNode(rerankingNode)
	g.AddNode(generationNode)

	// 添加边：chunking -> indexing -> retrieval -> reranking -> generation
	g.AddStart(chunkingNode)
	g.AddEdge(chunkingNode, indexingNode)
	g.AddEdge(indexingNode, retrievalNode)
	g.AddEdge(retrievalNode, rerankingNode)
	g.AddEdge(rerankingNode, generationNode)

	// 7. 编译 Graph
	runner, err := g.Compile()
	if err != nil {
		log.Fatalf("Failed to compile graph: %v", err)
	}

	// 8. 准备初始状态
	longDoc := `Rainy weather requires special preparation for your commute.
	First, always carry a waterproof jacket in your bag, as weather can change unexpectedly.
	Second, check the forecast before leaving home to plan your route accordingly.
	Third, a compact umbrella fits in most bags and provides essential protection.
	Fourth, choose covered walkways or sheltered routes when possible to minimize exposure.
	Finally, leave earlier than usual to account for slower traffic during heavy rain.`

	initialState := &RAGState{
		Query:          "How do I prepare for a rainy commute?",
		OriginalDoc:    longDoc,
		ConversationID: "session-001",
	}

	// 9. 运行 Graph
	fmt.Println("=== Starting RAG Pipeline ===")
	fmt.Printf("Question: %s\n\n", initialState.Query)

	finalState, err := runner.Run(ctx, initialState)
	if err != nil {
		log.Fatalf("Pipeline execution failed: %v", err)
	}

	// 10. 输出最终结果
	fmt.Println("\n=== Final Answer ===")
	fmt.Println(finalState.FinalAnswer)
}
