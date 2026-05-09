package tools

import "context"

// ToolFilter selects a subset of tools based on criteria.
type ToolFilter interface {
	Filter(ctx context.Context, tools []Tool) ([]Tool, error)
}

// ToolFilterFunc is a function adapter for ToolFilter.
type ToolFilterFunc func(ctx context.Context, tools []Tool) ([]Tool, error)

func (f ToolFilterFunc) Filter(ctx context.Context, tools []Tool) ([]Tool, error) {
	return f(ctx, tools)
}

// AllowOnly returns a filter that keeps only the named tools.
func AllowOnly(names ...string) ToolFilter {
	set := make(map[string]struct{}, len(names))
	for _, n := range names {
		set[n] = struct{}{}
	}
	return ToolFilterFunc(func(_ context.Context, tools []Tool) ([]Tool, error) {
		var result []Tool
		for _, t := range tools {
			if _, ok := set[t.Spec().Name]; ok {
				result = append(result, t)
			}
		}
		return result, nil
	})
}

// Disallow returns a filter that removes the named tools.
func Disallow(names ...string) ToolFilter {
	set := make(map[string]struct{}, len(names))
	for _, n := range names {
		set[n] = struct{}{}
	}
	return ToolFilterFunc(func(_ context.Context, tools []Tool) ([]Tool, error) {
		var result []Tool
		for _, t := range tools {
			if _, ok := set[t.Spec().Name]; !ok {
				result = append(result, t)
			}
		}
		return result, nil
	})
}

// And combines filters; a tool must pass all filters to be included.
func And(filters ...ToolFilter) ToolFilter {
	return ToolFilterFunc(func(ctx context.Context, ts []Tool) ([]Tool, error) {
		result := ts
		for _, f := range filters {
			var err error
			result, err = f.Filter(ctx, result)
			if err != nil {
				return nil, err
			}
		}
		return result, nil
	})
}

// Or combines filters; a tool passes if it passes any filter.
func Or(filters ...ToolFilter) ToolFilter {
	return ToolFilterFunc(func(ctx context.Context, ts []Tool) ([]Tool, error) {
		seen := make(map[string]struct{})
		var result []Tool
		for _, f := range filters {
			passed, err := f.Filter(ctx, ts)
			if err != nil {
				return nil, err
			}
			for _, t := range passed {
				name := t.Spec().Name
				if _, ok := seen[name]; !ok {
					seen[name] = struct{}{}
					result = append(result, t)
				}
			}
		}
		return result, nil
	})
}
