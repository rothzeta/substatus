// Package runner aggregates providers into a status snapshot.
package runner

import (
	"context"
	"sync"
	"time"

	"github.com/local/substatus/internal/status"
)

// Default interval between refresh cycles.
const DefaultInterval = 60 * time.Second

// Provider reports one provider's status. Fetch need not set Name.
type Provider interface {
	Name() string
	Fetch(ctx context.Context) status.Provider
}

// Runner refreshes all providers concurrently.
type Runner struct {
	Providers []Provider
	Interval  time.Duration
}

// New returns a Runner over the given providers.
func New(ps ...Provider) *Runner {
	return &Runner{Providers: ps, Interval: DefaultInterval}
}

// Refresh queries every provider in parallel and returns a snapshot in
// provider order. A failing provider never prevents the others from reporting.
func (r *Runner) Refresh(ctx context.Context) status.Snapshot {
	results := make([]status.Provider, len(r.Providers))
	var wg sync.WaitGroup
	for i, p := range r.Providers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = fetch(ctx, p)
		}()
	}
	wg.Wait()
	return status.Snapshot{Providers: results, CheckedAt: time.Now()}
}

func fetch(ctx context.Context, p Provider) status.Provider {
	res := p.Fetch(ctx)
	res.Name = p.Name()
	return res
}

// Watch refreshes immediately and then on every tick until ctx is cancelled,
// emitting each snapshot on the returned channel.
func (r *Runner) Watch(ctx context.Context) <-chan status.Snapshot {
	interval := r.Interval
	if interval <= 0 {
		interval = DefaultInterval
	}
	ch := make(chan status.Snapshot)
	go func() {
		defer close(ch)
		send := func(s status.Snapshot) bool {
			select {
			case ch <- s:
				return true
			case <-ctx.Done():
				return false
			}
		}
		if !send(r.Refresh(ctx)) {
			return
		}
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if !send(r.Refresh(ctx)) {
					return
				}
			}
		}
	}()
	return ch
}
