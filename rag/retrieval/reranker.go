package retrieval

import (
	"context"
	"sort"

	"github.com/go-kratos/blades/rag"
)

// CrossEncoderReranker 使用交叉编码器模型进行重排序。
// 交叉编码器直接对 (query, doc) 对进行评分，比向量相似度更精确。
// 注意：需要外部提供评分逻辑。
type CrossEncoderReranker struct {
	scorer func(ctx context.Context, query string, doc rag.Document) (float64, error)
	topK   int // 重排序后返回的文档数量
}

// NewCrossEncoderReranker 创建一个交叉编码器重排序器。
// scorer 函数用于计算查询和文档之间的相关性分数。
func NewCrossEncoderReranker(scorer func(ctx context.Context, query string, doc rag.Document) (float64, error), topK int) *CrossEncoderReranker {
	if topK <= 0 {
		topK = 10
	}
	return &CrossEncoderReranker{
		scorer: scorer,
		topK:   topK,
	}
}

// Rerank 对文档进行重排序。
func (r *CrossEncoderReranker) Rerank(ctx context.Context, query string, docs []rag.Document) ([]rag.Document, error) {
	if len(docs) == 0 {
		return docs, nil
	}

	if r.scorer == nil {
		// 如果没有提供评分函数，返回原始排序
		if r.topK > 0 && r.topK < len(docs) {
			return docs[:r.topK], nil
		}
		return docs, nil
	}

	// 更新分数
	reranked := make([]rag.Document, len(docs))
	for i, doc := range docs {
		score, err := r.scorer(ctx, query, doc)
		if err != nil {
			return nil, err
		}
		doc.Score = score
		reranked[i] = doc
	}

	// 排序
	sort.Slice(reranked, func(i, j int) bool {
		return reranked[i].Score > reranked[j].Score
	})

	// 返回 topK
	if r.topK > 0 && r.topK < len(reranked) {
		return reranked[:r.topK], nil
	}

	return reranked, nil
}

// LLMReranker 使用 LLM 直接判断文档相关性进行重排序。
type LLMReranker struct {
	// 可以扩展为使用 blades.ModelProvider 调用 LLM
	topK int
}

// NewLLMReranker 创建一个基于 LLM 的重排序器。
func NewLLMReranker(topK int) *LLMReranker {
	if topK <= 0 {
		topK = 10
	}
	return &LLMReranker{topK: topK}
}

// Rerank 使用 LLM 进行重排序（占位实现）。
func (r *LLMReranker) Rerank(ctx context.Context, query string, docs []rag.Document) ([]rag.Document, error) {
	// TODO: 调用 LLM 让其对每个文档进行相关性打分
	// 这里返回原始排序
	if r.topK > 0 && r.topK < len(docs) {
		return docs[:r.topK], nil
	}
	return docs, nil
}

// ReciprocalRankFusion 实现倒数排名融合（RRF），用于合并多个检索结果。
type ReciprocalRankFusion struct {
	k int // 平滑参数，通常取 60
}

// NewReciprocalRankFusion 创建一个 RRF 融合器。
func NewReciprocalRankFusion() *ReciprocalRankFusion {
	return &ReciprocalRankFusion{k: 60}
}

// Fuse 融合多个检索结果列表。
func (r *ReciprocalRankFusion) Fuse(resultLists ...[]rag.Document) []rag.Document {
	if len(resultLists) == 0 {
		return nil
	}

	// 计算每个文档的融合分数
	scores := make(map[string]float64)
	docMap := make(map[string]rag.Document)

	for _, results := range resultLists {
		for rank, doc := range results {
			// RRF 公式: score = 1 / (k + rank)
			rrfScore := 1.0 / float64(r.k+rank+1)
			scores[doc.ID] += rrfScore
			docMap[doc.ID] = doc
		}
	}

	// 转换为文档列表并排序
	fused := make([]rag.Document, 0, len(scores))
	for id, score := range scores {
		doc := docMap[id]
		doc.Score = score
		fused = append(fused, doc)
	}

	sort.Slice(fused, func(i, j int) bool {
		if fused[i].Score == fused[j].Score {
			return fused[i].ID < fused[j].ID
		}
		return fused[i].Score > fused[j].Score
	})

	return fused
}
