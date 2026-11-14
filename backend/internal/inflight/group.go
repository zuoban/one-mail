package inflight

import (
	"context"
	"sync"
	"time"
)

type call[Value any] struct {
	done    chan struct{}
	cancel  context.CancelFunc
	waiters int
	value   Value
	err     error
}

type Group[Key comparable, Value any] struct {
	mu     sync.Mutex
	calls  map[Key]*call[Value]
	shared uint64
}

func (g *Group[Key, Value]) Stats() (int, uint64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.calls), g.shared
}

func (g *Group[Key, Value]) Do(ctx context.Context, key Key, timeout time.Duration, work func(context.Context) (Value, error)) (Value, error) {
	var empty Value
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	g.mu.Lock()
	if g.calls == nil {
		g.calls = make(map[Key]*call[Value])
	}
	current := g.calls[key]
	if current == nil {
		workCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
		current = &call[Value]{done: make(chan struct{}), cancel: cancel}
		g.calls[key] = current
		go func() {
			value, err := work(workCtx)
			g.mu.Lock()
			current.value, current.err = value, err
			if g.calls[key] == current {
				delete(g.calls, key)
			}
			close(current.done)
			g.mu.Unlock()
			cancel()
		}()
	} else {
		g.shared++
	}
	current.waiters++
	g.mu.Unlock()
	defer func() {
		g.mu.Lock()
		current.waiters--
		if current.waiters == 0 {
			if g.calls[key] == current {
				delete(g.calls, key)
			}
			current.cancel()
		}
		g.mu.Unlock()
	}()
	select {
	case <-ctx.Done():
		return empty, ctx.Err()
	case <-current.done:
		if err := ctx.Err(); err != nil {
			return empty, err
		}
		return current.value, current.err
	}
}
