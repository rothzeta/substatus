// Package runner aggregates providers into a status snapshot.
package runner

import (
	"context"
	"sync"
	"time"

	"github.com/local/substatus/internal/provider"
	"github.com/local/substatus/internal/status"
)

// Default interval between refresh cycles.
const DefaultInterval = 60 * time.Second

// Runner refreshes all providers concurrently.
type Runner struct {
	Providers []provider.Provider
	Interval  time.Duration
	Now       func() time.Time
}

// New returns a Runner over the given providers.
func New(ps ...provider.Provider) *Runner {
	return &Runner{Providers: ps, Interval: DefaultInterval, Now: time.Now}
}

// Refresh queries every provider in parallel and returns a sorted snapshot.
// A failing provider never prevents the others from reporting.
func (r *Runner) Refresh(ctx context.Context) status.Snapshot {
	results := make([]status.Provider, len(r.Providers))
	var wg sync.WaitGroup
	for i, p := range r.Providers {
		wg.Add(1)
		go func(i int, p provider.Provider) {
			defer wg.Done()
			results[i] = toStatus(p.Name(), p.Fetch(ctx))
		}(i, p)
	}
	wg.Wait()
	snap := status.Snapshot{Providers: results, CheckedAt: r.Now()}
	snap.Sort()
	return snap
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

func toStatus(name string, r provider.Result) status.Provider {
	sp := status.Provider{
		Name:    name,
		Plan:    r.Plan,
		Note:    r.Note,
		Source:  r.Source,
		Quality: quality(r.Quality),
		Checked: time.Now(),
	}
	switch r.State {
	case provider.ResOK:
		sp.State = status.StateOK
	case provider.ResAuthMissing:
		sp.State = status.StateAuthMissing
	case provider.ResNotInstalled:
		sp.State = status.StateNotInstalled
	case provider.ResUnsupported:
		sp.State = status.StateUnsupported
	case provider.ResUnavailable:
		sp.State = status.StateUnavailable
	default:
		sp.State = status.StateError
	}
	if r.Err != nil {
		sp.Err = r.Err.Error()
	}
	for _, w := range r.Windows {
		sp.Windows = append(sp.Windows, status.Window{
			Label:    w.Label,
			Percent:  w.Percent,
			ResetsAt: w.ResetsAt,
			HasReset: w.HasReset,
		})
	}
	return sp
}

func quality(q provider.Quality) status.SourceQuality {
	switch q {
	case provider.QualityOfficial:
		return status.QualityOfficial
	case provider.QualityPrivate:
		return status.QualityPrivate
	case provider.QualityCLI:
		return status.QualityCLI
	case provider.QualityReverse:
		return status.QualityReverse
	}
	return status.QualityOfficial
}
