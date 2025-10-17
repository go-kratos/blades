package retrieval

import (
	"strings"
)

// Tokenize 将文本分词为小写词元列表。
func Tokenize(query string) []string {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return nil
	}
	return strings.Fields(query)
}
