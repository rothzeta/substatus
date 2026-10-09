package runner

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rothzeta/substatus/internal/status"
)

// fakeProvider returns a canned result after release is closed (or at once if
// release is nil), counting calls.
type fakeProvider struct {
	name    string
	result  status.Provider
	release chan struct{}
	calls   atomic.Int32
}

func (f *fakeProvider) Name() string { return f.name }

func (f *fakeProvider) Fetch(ctx context.Context) status.Provider {
	f.calls.Add(1)
	if f.release != nil {
		select {
		case <-f.release:
		case <-ctx.Done():
			return status.Provider{State: status.StateError}
		}
	}
	return f.result
}

func next(t *testing.T, ch <-chan status.Snapshot) status.Snapshot {
	t.Helper()
	select {
	case s, ok := <-ch:
		if !ok {
			t.Fatal("watch closed")
		}
		return s
	case <-time.After(2 * time.Second):
		t.Fatal("no snapshot")
	}
	panic("unreachable")
}

func TestWatchShowsFastProvidersBeforeSlowOnes(t *testing.T) {
	fast := &fakeProvider{name: "Fast", result: status.Provider{State: status.StateOK}}
	slow := &fakeProvider{name: "Slow", result: status.Provider{State: status.StateOK}, release: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := New(time.Hour, slow, fast).Watch(ctx, nil)

	first := next(t, ch)
	if first.Providers[0].Name != "Slow" || first.Providers[0].State != status.StateLoading ||
		first.Providers[1].State != status.StateLoading || !first.CheckedAt.IsZero() {
		t.Fatalf("first = %+v", first)
	}
	partial := next(t, ch)
	if partial.Providers[0].State != status.StateLoading || partial.Providers[1].State != status.StateOK ||
		partial.Providers[1].Name != "Fast" || !partial.CheckedAt.IsZero() {
		t.Fatalf("partial = %+v", partial)
	}
	close(slow.release)
	done := next(t, ch)
	if done.Providers[0].State != status.StateOK || done.CheckedAt.IsZero() || done.Refreshing {
		t.Fatalf("done = %+v", done)
	}
}

func TestWatchRefreshesOnRequestWithoutOverlap(t *testing.T) {
	p := &fakeProvider{name: "A", result: status.Provider{State: status.StateOK, Plan: "kept"}}
	refresh := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := New(time.Hour, p).Watch(ctx, refresh)
	next(t, ch)
	next(t, ch) // cycle 1 complete

	p.release = make(chan struct{})
	refresh <- struct{}{}
	started := next(t, ch)
	if !started.Refreshing || started.Providers[0].Plan != "kept" {
		t.Fatalf("refresh start = %+v; previous data must stay visible", started)
	}
	refresh <- struct{}{} // ignored: a cycle is in flight
	close(p.release)
	if done := next(t, ch); done.Refreshing {
		t.Fatalf("done = %+v", done)
	}
	if calls := p.calls.Load(); calls != 2 {
		t.Fatalf("fetch calls = %d, want 2", calls)
	}
}

func TestWatchRefreshesOnTicks(t *testing.T) {
	p := &fakeProvider{name: "A", result: status.Provider{State: status.StateOK}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := New(20*time.Millisecond, p).Watch(ctx, nil)
	for range 4 { // two full cycles: start + result each
		next(t, ch)
	}
	if calls := p.calls.Load(); calls < 2 {
		t.Fatalf("fetch calls = %d, want at least 2", calls)
	}
}

func TestWatchStopsOnCancel(t *testing.T) {
	p := &fakeProvider{name: "A", release: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	ch := New(10*time.Millisecond, p).Watch(ctx, nil)
	next(t, ch)
	cancel()
	deadline := time.After(time.Second)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return
			}
		case <-deadline:
			t.Fatal("watch did not stop after cancel")
		}
	}
}

func TestRefreshReturnsCompleteSnapshotInOrder(t *testing.T) {
	ok := &fakeProvider{name: "Zeta", result: status.Provider{
		State: status.StateOK, Windows: []status.Window{{Label: "weekly", Percent: 28}},
	}}
	bad := &fakeProvider{name: "Alpha", result: status.Provider{State: status.StateError, Note: "boom"}}
	snap := New(time.Hour, ok, bad).Refresh(context.Background())

	if len(snap.Providers) != 2 || snap.Providers[0].Name != "Zeta" || snap.Providers[1].Name != "Alpha" {
		t.Fatalf("providers = %+v", snap.Providers)
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

func TestRefreshWithoutProvidersCompletes(t *testing.T) {
	if snap := New(time.Hour).Refresh(context.Background()); snap.CheckedAt.IsZero() {
		t.Fatal("empty refresh never completed")
	}
}

type panicProvider struct{}

func (panicProvider) Name() string                          { return "Boom" }
func (panicProvider) Fetch(context.Context) status.Provider { panic("boom") }

func TestPanickingProviderBecomesErrorRow(t *testing.T) {
	snap := New(time.Hour, panicProvider{}).Refresh(context.Background())
	if p := snap.Providers[0]; p.Name != "Boom" || p.State != status.StateError {
		t.Fatalf("got %+v", p)
	}
}

func TestWatchClosesOnlyAfterFetchesReturn(t *testing.T) {
	var returned atomic.Bool
	p := &blockingProvider{returned: &returned}
	ctx, cancel := context.WithCancel(context.Background())
	ch := New(time.Hour, p).Watch(ctx, nil)
	next(t, ch)
	cancel()
	for range ch {
	}
	if !returned.Load() {
		t.Fatal("watch closed while a fetch was still running")
	}
}

// blockingProvider blocks until cancelled, then lingers briefly before
// returning, like a CLI being killed.
type blockingProvider struct{ returned *atomic.Bool }

func (blockingProvider) Name() string { return "Slow" }
func (b *blockingProvider) Fetch(ctx context.Context) status.Provider {
	<-ctx.Done()
	time.Sleep(50 * time.Millisecond)
	b.returned.Store(true)
	return status.Provider{}
}
