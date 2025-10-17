package store

import (
	"github.com/go-kratos/blades/rag"
)

// MatchFilters 检查文档是否匹配过滤条件
func MatchFilters(doc rag.Document, filters map[string]string) bool {
	if len(filters) == 0 {
		return true
	}

	for key, expected := range filters {
		value, ok := doc.Metadata[key]
		if !ok {
			return false
		}
		str, ok := value.(string)
		if !ok || str != expected {
			return false
		}
	}

	return true
}
