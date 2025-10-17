package chunking

import (
	"strings"
	"unicode"
)

// FixedSizeChunker 按固定大小和重叠度分割文本。
type FixedSizeChunker struct {
	ChunkSize int // 每个块的最大字符数
	Overlap   int // 块之间的重叠字符数
}

// NewFixedSizeChunker 创建一个固定大小的分块器。
func NewFixedSizeChunker(chunkSize, overlap int) *FixedSizeChunker {
	if chunkSize <= 0 {
		chunkSize = 500
	}
	if overlap < 0 || overlap >= chunkSize {
		overlap = chunkSize / 10
	}
	return &FixedSizeChunker{
		ChunkSize: chunkSize,
		Overlap:   overlap,
	}
}

// Split 将文本分割成多个块。
func (c *FixedSizeChunker) Split(content string) []string {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return nil
	}

	runes := []rune(trimmed)
	if len(runes) <= c.ChunkSize {
		return []string{trimmed}
	}

	var chunks []string
	start := 0

	for start < len(runes) {
		end := start + c.ChunkSize
		if end > len(runes) {
			end = len(runes)
		}

		// 尝试在单词边界处截断
		if end < len(runes) {
			// 向前查找空白字符，最多向前查找 50 个字符
			foundSpace := false
			limit := end - 50
			if limit < start {
				limit = start
			}
			for i := end - 1; i >= limit; i-- {
				if unicode.IsSpace(runes[i]) {
					end = i
					foundSpace = true
					break
				}
			}
			// 如果没找到空白，保持原始 end（强制截断）
			if !foundSpace {
				// 不做任何调整，直接使用 end
			}
		}

		chunk := strings.TrimSpace(string(runes[start:end]))
		if chunk != "" {
			chunks = append(chunks, chunk)
		}

		// 下一个块从当前块减去重叠部分开始
		nextStart := end - c.Overlap
		if nextStart < 0 {
			nextStart = 0
		}

		// 确保至少前进到下一个有意义的位置
		if nextStart <= start {
			nextStart = end // 没有重叠，直接从 end 开始
		}

		// 如果已经到达末尾，退出
		if nextStart >= len(runes) {
			break
		}

		start = nextStart
	}

	return chunks
}

// SentenceChunker 按句子分割文本，尽量保持每个块不超过指定大小。
type SentenceChunker struct {
	MaxChunkSize int // 每个块的最大字符数
}

// NewSentenceChunker 创建一个按句子分割的分块器。
func NewSentenceChunker(maxChunkSize int) *SentenceChunker {
	if maxChunkSize <= 0 {
		maxChunkSize = 1000
	}
	return &SentenceChunker{
		MaxChunkSize: maxChunkSize,
	}
}

// Split 将文本按句子分割成多个块。
func (c *SentenceChunker) Split(content string) []string {
	if content == "" {
		return nil
	}

	content = strings.TrimSpace(content)
	if len(content) <= c.MaxChunkSize {
		return []string{content}
	}

	// 简单的句子分割（可以用更复杂的 NLP 库替换）
	sentences := splitSentences(content)
	if len(sentences) == 0 {
		return []string{content}
	}

	var chunks []string
	var currentChunk strings.Builder

	for _, sentence := range sentences {
		sentence = strings.TrimSpace(sentence)
		if sentence == "" {
			continue
		}

		// 如果当前句子本身就超过最大大小，直接作为一个块
		if len(sentence) > c.MaxChunkSize {
			if currentChunk.Len() > 0 {
				chunks = append(chunks, currentChunk.String())
				currentChunk.Reset()
			}
			chunks = append(chunks, sentence)
			continue
		}

		// 如果添加这个句子会超过最大大小，先保存当前块
		if currentChunk.Len()+len(sentence)+1 > c.MaxChunkSize {
			if currentChunk.Len() > 0 {
				chunks = append(chunks, currentChunk.String())
				currentChunk.Reset()
			}
		}

		if currentChunk.Len() > 0 {
			currentChunk.WriteString(" ")
		}
		currentChunk.WriteString(sentence)
	}

	if currentChunk.Len() > 0 {
		chunks = append(chunks, currentChunk.String())
	}

	return chunks
}

// splitSentences 简单的句子分割器，按句号、问号、感叹号分割。
func splitSentences(text string) []string {
	var sentences []string
	var current strings.Builder

	runes := []rune(text)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		current.WriteRune(r)

		// 检查是否是句子结束符
		if r == '.' || r == '?' || r == '!' || r == '。' || r == '？' || r == '！' {
			// 检查下一个字符是否是空白或文本结束
			if i+1 >= len(runes) || unicode.IsSpace(runes[i+1]) {
				sentence := strings.TrimSpace(current.String())
				if sentence != "" {
					sentences = append(sentences, sentence)
				}
				current.Reset()
			}
		}
	}

	// 添加剩余的文本
	if current.Len() > 0 {
		sentence := strings.TrimSpace(current.String())
		if sentence != "" {
			sentences = append(sentences, sentence)
		}
	}

	return sentences
}
