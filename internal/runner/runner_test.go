package runner

import (
	"context"
	"testing"
	"time"

	"github.com/local/substatus/internal/status"
)

// fakeProvider returns a canned result, or blocks until the context ends.
type fakeProvider struct {
	name    string
	result  status.Provider
	delay   time.Duration
	started chan struct{}
}

func (f *fakeProvider) Name() string { return f.name }

func (f *fakeProvider) Fetch(ctx context.Context) status.Provider {
	if f.started != nil {
		select {
		case f.started <- struct{}{}:
		default:
		}
	}
	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return status.Provider{State: status.StateError, Note: ctx.Err().Error()}
		}
	}
	return f.result
}

func TestRefreshKeepsOrderAndPartialFailures(t *testing.T) {
	ok := &fakeProvider{name: "Zeta", result: status.Provider{
		State: status.StateOK, Plan: "max",
		Windows: []status.Window{{Label: "weekly", Percent: 28}},
	}}
	bad := &fakeProvider{name: "Alpha", result: status.Provider{
		State: status.StateError, Note: "boom",
	}}
	r := New(ok, bad)
	snap := r.Refresh(context.Background())

	if len(snap.Providers) != 2 {
		t.Fatalf("providers = %d", len(snap.Providers))
	}
	if snap.Providers[0].Name != "Zeta" || snap.Providers[1].Name != "Alpha" {
		t.Fatalf("order = %s,%s", snap.Providers[0].Name, snap.Providers[1].Name)
	}
	if snap.Providers[1].State != status.StateError || snap.Providers[1].Note != "boom" {
		t.Errorf("error provider not preserved: %+v", snap.Providers[1])
	}
	if snap.Providers[0].State != status.StateOK || len(snap.Providers[0].Windows) != 1 {
		t.Errorf("ok provider wrong: %+v", snap.Providers[0])
	}
	if snap.CheckedAt.IsZero() {
		t.Error("CheckedAt not set")
	}
}

func TestRefreshRunsProvidersConcurrently(t *testing.T) {
	started := make(chan struct{}, 2)
	a := &fakeProvider{name: "A", delay: 100 * time.Millisecond, started: started, result: status.Provider{State: status.StateOK}}
	b := &fakeProvider{name: "B", delay: 100 * time.Millisecond, started: started, result: status.Provider{State: status.StateOK}}

	begin := time.Now()
	New(a, b).Refresh(context.Background())
	elapsed := time.Since(begin)
	if elapsed > 180*time.Millisecond {
		t.Fatalf("providers ran serially: %v", elapsed)
	}
	if len(started) != 2 {
		t.Fatalf("started = %d, want 2", len(started))
	}
}

func TestWatchEmitsImmediatelyThenOnTicks(t *testing.T) {
	p := &fakeProvider{name: "A", result: status.Provider{State: status.StateOK}}
	r := New(p)
	r.Interval = 20 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := r.Watch(ctx)

	first := <-ch
	if first.CheckedAt.IsZero() {
		t.Fatal("first snapshot missing timestamp")
	}
	select {
	case <-ch:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("no second snapshot within interval")
	}
}

func TestWatchStopsOnCancel(t *testing.T) {
	p := &fakeProvider{name: "A", result: status.Provider{State: status.StateOK}}
	r := New(p)
	r.Interval = 10 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	ch := r.Watch(ctx)
	<-ch
	cancel()
	deadline := time.After(500 * time.Millisecond)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return // channel closed
			}
		case <-deadline:
			t.Fatal("watch did not stop after cancel")
		}
	}
}
