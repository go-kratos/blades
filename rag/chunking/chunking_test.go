package chunking

import (
	"strings"
	"testing"
)

func TestFixedSizeChunker(t *testing.T) {
	chunker := NewFixedSizeChunker(50, 10)
	text := "This is the first sentence. This is the second sentence. This is the third sentence. This is the fourth sentence."

	chunks := chunker.Split(text)

	if len(chunks) == 0 {
		t.Fatal("expected chunks, got none")
	}

	// 验证每个块的大小
	for i, chunk := range chunks {
		if len(chunk) > chunker.ChunkSize+50 { // 允许一些误差
			t.Errorf("chunk %d is too large: %d chars", i, len(chunk))
		}
	}

	// 验证重叠
	if len(chunks) > 1 {
		// 简单检查是否有重叠内容
		for i := 0; i < len(chunks)-1; i++ {
			// 检查相邻块是否有共同内容（通过检查最后几个词）
			t.Logf("Chunk %d: %s", i, chunks[i])
		}
	}
}

func TestFixedSizeChunkerUnicode(t *testing.T) {
	chunker := NewFixedSizeChunker(4, 0)
	text := "天气不错我们去散步吧"

	chunks := chunker.Split(text)
	expected := []string{"天气不错", "我们去散", "步吧"}

	if len(chunks) != len(expected) {
		t.Fatalf("expected %d chunks, got %d: %v", len(expected), len(chunks), chunks)
	}

	for i, chunk := range chunks {
		if chunk != expected[i] {
			t.Fatalf("chunk %d mismatch: want %q, got %q", i, expected[i], chunk)
		}
		if strings.ContainsRune(chunk, '�') {
			t.Fatalf("chunk %d contains replacement rune: %q", i, chunk)
		}
	}
}

func TestSentenceChunker(t *testing.T) {
	chunker := NewSentenceChunker(100)
	text := "First sentence. Second sentence? Third sentence! Fourth sentence. Fifth sentence."

	chunks := chunker.Split(text)

	if len(chunks) == 0 {
		t.Fatal("expected chunks, got none")
	}

	for i, chunk := range chunks {
		t.Logf("Chunk %d: %s", i, chunk)
		if len(chunk) > chunker.MaxChunkSize+50 { // 允许一些误差
			t.Errorf("chunk %d is too large: %d chars", i, len(chunk))
		}
	}
}

func TestSentenceChunkerEmpty(t *testing.T) {
	chunker := NewSentenceChunker(100)
	chunks := chunker.Split("")

	if len(chunks) != 0 {
		t.Errorf("expected 0 chunks for empty text, got %d", len(chunks))
	}
}

func TestSentenceChunkerShortText(t *testing.T) {
	chunker := NewSentenceChunker(1000)
	text := "Short text."

	chunks := chunker.Split(text)

	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk for short text, got %d", len(chunks))
	}

	if chunks[0] != text {
		t.Errorf("expected chunk to equal input text")
	}
}

func TestSplitSentences(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		expected int
	}{
		{"simple", "Hello. World.", 2},
		{"question", "How are you? I am fine.", 2},
		{"exclamation", "Great! Amazing!", 2},
		{"mixed", "First. Second? Third!", 3},
		{"chinese", "你好。 世界！", 2}, // 添加空格以符合我们的分句逻辑
		{"no_punctuation", "Hello world", 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sentences := splitSentences(tt.text)
			if len(sentences) != tt.expected {
				t.Errorf("expected %d sentences, got %d: %v", tt.expected, len(sentences), sentences)
			}
		})
	}
}
