package retrieval

import (
	"testing"

	"github.com/go-kratos/blades/rag"
)

func TestBM25Scorer(t *testing.T) {
	docs := []rag.Document{
		{ID: "doc-1", Content: "the quick brown fox jumps over the lazy dog"},
		{ID: "doc-2", Content: "the fox is quick and smart"},
		{ID: "doc-3", Content: "dogs are lazy but friendly"},
	}

	scorer := NewBM25Scorer()
	scorer.Index(docs)

	// 测试查询
	query := "quick fox"

	scores := make(map[string]float64)
	for _, doc := range docs {
		score := scorer.Score(query, doc)
		scores[doc.ID] = score
	}

	// doc-1 和 doc-2 应该得分更高，因为它们包含 "quick" 和 "fox"
	if scores["doc-1"] == 0 {
		t.Error("expected doc-1 to have non-zero score")
	}
	if scores["doc-2"] == 0 {
		t.Error("expected doc-2 to have non-zero score")
	}
	if scores["doc-3"] != 0 {
		t.Error("expected doc-3 to have zero score (no matching terms)")
	}

	t.Logf("Scores: doc-1=%.3f, doc-2=%.3f, doc-3=%.3f",
		scores["doc-1"], scores["doc-2"], scores["doc-3"])
}

func TestBM25ScorerEmptyQuery(t *testing.T) {
	docs := []rag.Document{
		{ID: "doc-1", Content: "some content"},
	}

	scorer := NewBM25Scorer()
	scorer.Index(docs)

	score := scorer.Score("", docs[0])
	if score != 0 {
		t.Errorf("expected zero score for empty query, got %.3f", score)
	}
}

func TestBM25ScorerSingleDocument(t *testing.T) {
	docs := []rag.Document{
		{ID: "doc-1", Content: "the quick brown fox"},
	}

	scorer := NewBM25Scorer()
	scorer.Index(docs)

	score := scorer.Score("quick fox", docs[0])
	if score == 0 {
		t.Error("expected non-zero score")
	}

	t.Logf("Score: %.3f", score)
}

func TestBM25NormalizeScore(t *testing.T) {
	scorer := NewBM25Scorer()

	// 测试归一化
	rawScore := 15.5
	normalized := scorer.NormalizeScore(rawScore, 3) // 3个查询词

	if normalized < 0 || normalized > 1 {
		t.Errorf("expected normalized score in [0,1], got %.3f", normalized)
	}

	t.Logf("Raw: %.3f, Normalized: %.3f", rawScore, normalized)
}
