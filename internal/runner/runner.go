// Package runner aggregates providers into status snapshots.
package runner

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/rothzeta/substatus/internal/status"
)

// DefaultInterval is the default time between refresh cycles.
const DefaultInterval = 5 * time.Minute

// Provider reports one provider's status. Fetch need not set Name.
type Provider interface {
	Name() string
	Fetch(ctx context.Context) status.Provider
}

// Runner refreshes providers concurrently on an interval.
type Runner struct {
	providers []Provider
	interval  time.Duration
}

// New returns a Runner over providers, in display order. A non-positive
// interval means DefaultInterval.
func New(interval time.Duration, providers ...Provider) Runner {
	if interval <= 0 {
		interval = DefaultInterval
	}
	return Runner{providers: slices.Clone(providers), interval: interval}
}

// Refresh runs one full cycle and returns its final snapshot. If ctx ends
// first, it returns the latest partial snapshot.
func (r Runner) Refresh(ctx context.Context) status.Snapshot {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var last status.Snapshot
	for snap := range r.Watch(ctx, nil) {
		last = snap
		if !snap.CheckedAt.IsZero() {
			break
		}
	}
	return last
}

// Watch starts a refresh cycle immediately, then on every tick and on every
// receive from refresh (which may be nil). It emits a snapshot right away with
// every provider loading, then a new one whenever a cycle starts or any
// provider finishes, so one slow provider never delays the others. A cycle
// never overlaps the previous one. The channel closes when ctx ends and every
// in-flight fetch has returned, so provider subprocesses are reaped by then.
func (r Runner) Watch(ctx context.Context, refresh <-chan struct{}) <-chan status.Snapshot {
	out := make(chan status.Snapshot)
	go func() {
		var fetches sync.WaitGroup
		defer close(out)
		defer fetches.Wait()
		type result struct {
			i int
			p status.Provider
		}
		results := make(chan result)
		rows := make([]status.Provider, len(r.providers))
		for i, p := range r.providers {
			rows[i] = status.Provider{Name: p.Name(), State: status.StateLoading}
		}
		var checkedAt time.Time
		pending := 0

		emit := func() bool {
			snap := status.Snapshot{Providers: slices.Clone(rows), CheckedAt: checkedAt, Refreshing: pending > 0}
			select {
			case out <- snap:
				return true
			case <-ctx.Done():
				return false
			}
		}
		start := func() bool {
			if pending > 0 {
				return true // the cycle in flight will deliver fresh data
			}
			if len(r.providers) == 0 {
				checkedAt = time.Now()
			}
			pending = len(r.providers)
			fetches.Add(len(r.providers))
			for i, p := range r.providers {
				go func() {
					defer fetches.Done()
					res := fetch(ctx, p)
					select {
					case results <- result{i, res}:
					case <-ctx.Done():
					}
				}()
			}
			return emit()
		}

		if !start() {
			return
		}
		tick := time.NewTicker(r.interval)
		defer tick.Stop()
		for {
			ok := true
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				ok = start()
			case <-refresh:
				ok = start()
			case res := <-results:
				rows[res.i] = res.p
				if pending--; pending == 0 {
					checkedAt = time.Now()
				}
				ok = emit()
			}
			if !ok {
				return
			}
		}
	}()
	return out
}

// fetch runs one provider check, reporting a panic as an error row instead of
// crashing the program with the terminal still in raw mode.
func fetch(ctx context.Context, p Provider) (res status.Provider) {
	defer func() {
		if recover() != nil {
			res = status.Provider{State: status.StateError, Note: "provider check crashed"}
		}
		res.Name = p.Name()
	}()
	return p.Fetch(ctx)
}
