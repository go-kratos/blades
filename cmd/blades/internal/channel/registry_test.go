package channel

import (
	"context"
	"errors"
	"testing"
)

func TestRegistry_Register_Start(t *testing.T) {
	r := NewRegistry()
	done := make(chan struct{})
	r.Register("test", func(cfg interface{}) (Channel, error) {
		return &blockChannel{name: "test", done: done}, nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		_ = r.Start(ctx, "test", nil, func(ctx context.Context, sid, text string, w Writer) (string, error) {
			return "", nil
		})
	}()
	cancel()
	<-done
}

func TestRegistry_Start_unknown(t *testing.T) {
	r := NewRegistry()
	err := r.Start(context.Background(), "missing", nil, nil)
	if err == nil {
		t.Fatal("expected error for unknown channel")
	}
}

func TestRegistry_Start_factoryError(t *testing.T) {
	r := NewRegistry()
	wantErr := errors.New("config required")
	r.Register("bad", func(cfg interface{}) (Channel, error) {
		return nil, wantErr
	})
	err := r.Start(context.Background(), "bad", nil, nil)
	if err != wantErr {
		t.Errorf("got %v", err)
	}
}

func TestRegistry_Names(t *testing.T) {
	r := NewRegistry()
	r.Register("a", func(interface{}) (Channel, error) { return nil, nil })
	r.Register("b", func(interface{}) (Channel, error) { return nil, nil })
	names := r.Names()
	if len(names) != 2 {
		t.Fatalf("len(Names) = %d", len(names))
	}
	seen := make(map[string]bool)
	for _, n := range names {
		seen[n] = true
	}
	if !seen["a"] || !seen["b"] {
		t.Errorf("Names = %v", names)
	}
}

type blockChannel struct {
	name string
	done chan struct{}
}

func (c *blockChannel) Name() string { return c.name }
func (c *blockChannel) Start(ctx context.Context, handler StreamHandler) error {
	<-ctx.Done()
	close(c.done)
	return nil
}
