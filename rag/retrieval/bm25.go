package retrieval

import (
	"math"

	"github.com/go-kratos/blades/rag"
)

// BM25Scorer 实现 BM25 算法，用于计算查询和文档之间的相关性分数。
type BM25Scorer struct {
	k1 float64 // 词频饱和参数，通常取 1.2-2.0
	b  float64 // 长度归一化参数，通常取 0.75

	// 统计信息
	avgDocLen float64
	docCount  int
	docFreq   map[string]int    // 每个词出现在多少个文档中
	docLens   map[string]int    // 每个文档的长度
	idf       map[string]float64 // 每个词的 IDF 值
}

// NewBM25Scorer 创建一个 BM25 评分器。
func NewBM25Scorer() *BM25Scorer {
	return &BM25Scorer{
		k1:      1.5,
		b:       0.75,
		docFreq: make(map[string]int),
		docLens: make(map[string]int),
		idf:     make(map[string]float64),
	}
}

// Index 为文档建立 BM25 索引。
func (s *BM25Scorer) Index(docs []rag.Document) {
	s.docCount = len(docs)
	s.docFreq = make(map[string]int)
	s.docLens = make(map[string]int)

	var totalLen int

	// 第一遍：计算文档频率和文档长度
	for _, doc := range docs {
		tokens := Tokenize(doc.Content)
		s.docLens[doc.ID] = len(tokens)
		totalLen += len(tokens)

		// 使用 map 去重，确保每个词在每个文档中只计数一次
		seen := make(map[string]bool)
		for _, token := range tokens {
			if !seen[token] {
				s.docFreq[token]++
				seen[token] = true
			}
		}
	}

	// 计算平均文档长度
	if s.docCount > 0 {
		s.avgDocLen = float64(totalLen) / float64(s.docCount)
	}

	// 第二遍：计算 IDF 值
	for term, df := range s.docFreq {
		// IDF = log((N - df + 0.5) / (df + 0.5) + 1)
		s.idf[term] = math.Log((float64(s.docCount)-float64(df)+0.5)/(float64(df)+0.5) + 1.0)
	}
}

// Score 计算查询和文档之间的 BM25 分数。
func (s *BM25Scorer) Score(query string, doc rag.Document) float64 {
	queryTokens := Tokenize(query)
	docTokens := Tokenize(doc.Content)

	// 计算词频
	termFreq := make(map[string]int)
	for _, token := range docTokens {
		termFreq[token]++
	}

	docLen := s.docLens[doc.ID]
	if docLen == 0 {
		docLen = len(docTokens)
	}

	var score float64
	for _, queryToken := range queryTokens {
		if queryToken == "" {
			continue
		}

		tf := float64(termFreq[queryToken])
		if tf == 0 {
			continue
		}

		idf := s.idf[queryToken]
		if idf == 0 {
			// 如果词不在索引中，使用默认 IDF
			idf = math.Log(float64(s.docCount) + 1.0)
		}

		// BM25 公式
		// score = IDF * (tf * (k1 + 1)) / (tf + k1 * (1 - b + b * (docLen / avgDocLen)))
		numerator := tf * (s.k1 + 1)
		denominator := tf + s.k1*(1-s.b+s.b*float64(docLen)/s.avgDocLen)
		score += idf * (numerator / denominator)
	}

	return score
}

// NormalizeScore 将 BM25 分数归一化到 0-1 范围（可选）。
func (s *BM25Scorer) NormalizeScore(score float64, queryTokenCount int) float64 {
	if queryTokenCount == 0 {
		return 0
	}
	// 简单的归一化：除以查询词数量
	// 实际场景中可以根据统计数据调整
	maxPossibleScore := float64(queryTokenCount) * 10.0 // 经验值
	normalized := score / maxPossibleScore
	if normalized > 1.0 {
		normalized = 1.0
	}
	return normalized
}
