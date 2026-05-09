package memory

// RecallOption configures a Recall call.
type RecallOption func(*recallOptions)

type recallOptions struct {
	limit  int
	filter map[string]any
}

// WithLimit sets the maximum number of entries to recall.
func WithLimit(n int) RecallOption {
	return func(o *recallOptions) {
		o.limit = n
	}
}

// WithFilter restricts recall by metadata filter.
func WithFilter(filter map[string]any) RecallOption {
	return func(o *recallOptions) {
		o.filter = filter
	}
}

// RememberOption configures a Remember call.
type RememberOption func(*rememberOptions)

type rememberOptions struct {
	metadata map[string]any
}

// WithMetadata attaches metadata to the memory entry.
func WithMetadata(metadata map[string]any) RememberOption {
	return func(o *rememberOptions) {
		o.metadata = metadata
	}
}

// ForgetOption configures a Forget call.
type ForgetOption func(*forgetOptions)

type forgetOptions struct {
	ids    []string
	filter map[string]any
}

// WithIDs restricts forget to specific entry IDs.
func WithIDs(ids ...string) ForgetOption {
	return func(o *forgetOptions) {
		o.ids = append(o.ids, ids...)
	}
}

// WithForgetFilter restricts forget by metadata filter.
func WithForgetFilter(filter map[string]any) ForgetOption {
	return func(o *forgetOptions) {
		o.filter = filter
	}
}

// ApplyRecallOptions applies options and returns the resolved config.
func ApplyRecallOptions(opts []RecallOption) (limit int, filter map[string]any) {
	o := &recallOptions{limit: 10}
	for _, opt := range opts {
		opt(o)
	}
	return o.limit, o.filter
}

// ApplyForgetOptions applies options and returns the resolved config.
func ApplyForgetOptions(opts []ForgetOption) (ids []string, filter map[string]any) {
	o := &forgetOptions{}
	for _, opt := range opts {
		opt(o)
	}
	return o.ids, o.filter
}
