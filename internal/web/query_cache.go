package web

import (
	"context"
	"slices"
	"strings"
	"sync"
	"time"
)

const queryCacheLimit = 256
const queryCacheTTL = 15 * time.Minute

type queryEmbedding struct {
	values  []float32
	model   string
	err     error
	expires time.Time
	done    chan struct{}
}

type queryCache struct {
	mu      sync.Mutex
	entries map[string]*queryEmbedding
	pending map[string]*queryEmbedding
}

func (c *queryCache) embed(ctx context.Context, query string, embed func(context.Context, string) ([]float32, string, error)) ([]float32, string, bool, error) {
	key := strings.TrimSpace(query)
	c.mu.Lock()
	if value := c.entries[key]; value != nil && time.Now().Before(value.expires) {
		values, model := slices.Clone(value.values), value.model
		c.mu.Unlock()
		return values, model, true, nil
	}
	if value := c.pending[key]; value != nil {
		c.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, "", false, ctx.Err()
		case <-value.done:
			return slices.Clone(value.values), value.model, true, value.err
		}
	}
	if c.pending == nil {
		c.pending = make(map[string]*queryEmbedding)
	}
	value := &queryEmbedding{done: make(chan struct{})}
	c.pending[key] = value
	c.mu.Unlock()
	values, model, err := embed(ctx, key)
	c.mu.Lock()
	value.values = slices.Clone(values)
	value.model = model
	value.err = err
	delete(c.pending, key)
	if err == nil && len(values) > 0 {
		if c.entries == nil {
			c.entries = make(map[string]*queryEmbedding)
		}
		now := time.Now()
		for k, v := range c.entries {
			if !now.Before(v.expires) {
				delete(c.entries, k)
			}
		}
		if len(c.entries) >= queryCacheLimit {
			var oldest string
			var expires time.Time
			for k, v := range c.entries {
				if expires.IsZero() || v.expires.Before(expires) {
					oldest = k
					expires = v.expires
				}
			}
			delete(c.entries, oldest)
		}
		value.expires = now.Add(queryCacheTTL)
		c.entries[key] = value
	}
	close(value.done)
	c.mu.Unlock()
	return values, model, false, err
}
