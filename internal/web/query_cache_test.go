package web

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestQueryCacheReuseAndExpiry(t *testing.T) {
	var c queryCache
	calls := 0
	embed := func(_ context.Context, q string) ([]float32, string, error) {
		calls++
		return []float32{1, 0}, "m", nil
	}
	values, _, cached, err := c.embed(context.Background(), " 美女 ", embed)
	if err != nil || cached {
		t.Fatal(cached, err)
	}
	values[0] = 0
	values, model, cached, err := c.embed(context.Background(), "美女", embed)
	if err != nil || !cached || calls != 1 || values[0] != 1 || model != "m" {
		t.Fatal(values, model, cached, calls, err)
	}
	c.entries["美女"].expires = time.Now().Add(-time.Second)
	_, _, cached, err = c.embed(context.Background(), "美女", embed)
	if err != nil || cached || calls != 2 {
		t.Fatal(cached, calls, err)
	}
	for i := 0; i < queryCacheLimit+20; i++ {
		c.embed(context.Background(), fmt.Sprint(i), embed)
	}
	if len(c.entries) > queryCacheLimit {
		t.Fatal("unbounded cache", len(c.entries))
	}
}

func TestQueryCacheDoesNotCacheFailures(t *testing.T) {
	var c queryCache
	calls := 0
	embed := func(context.Context, string) ([]float32, string, error) {
		calls++
		return nil, "", errors.New("offline")
	}
	for i := 0; i < 2; i++ {
		if _, _, _, err := c.embed(context.Background(), "美女", embed); err == nil {
			t.Fatal("missing error")
		}
	}
	if calls != 2 || len(c.entries) != 0 {
		t.Fatal("failure cached", calls)
	}
}

func TestQueryCacheCoalescesConcurrentRequests(t *testing.T) {
	var c queryCache
	var calls atomic.Int32
	gate := make(chan struct{})
	started := make(chan struct{})
	embed := func(context.Context, string) ([]float32, string, error) {
		calls.Add(1)
		close(started)
		<-gate
		return []float32{1, 0}, "m", nil
	}
	var wg sync.WaitGroup
	run := func() {
		defer wg.Done()
		values, _, _, err := c.embed(context.Background(), "美女", embed)
		if err != nil || len(values) != 2 {
			t.Error(values, err)
		}
	}
	wg.Add(1)
	go run()
	<-started
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go run()
	}
	close(gate)
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatal("duplicate embedding requests", calls.Load())
	}
}

func TestQueryCacheWaitCanBeCanceled(t *testing.T) {
	var c queryCache
	gate := make(chan struct{})
	started := make(chan struct{})
	done := make(chan struct{})
	embed := func(context.Context, string) ([]float32, string, error) {
		close(started)
		<-gate
		return []float32{1, 0}, "m", nil
	}
	go func() { c.embed(context.Background(), "美女", embed); close(done) }()
	<-started
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, _, err := c.embed(ctx, "美女", embed); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	close(gate)
	<-done
}
