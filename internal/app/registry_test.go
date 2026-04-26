package app

import (
	"context"
	"errors"
	"reflect"
	"sync/atomic"
	"testing"
)

// fakeDomain is a minimal Domain for exercising Registry semantics.
type fakeDomain struct {
	name     string
	startErr error

	started  *atomic.Int64 // monotonic start order; nil if not tracked
	stopped  *atomic.Int64 // monotonic stop order; nil if not tracked
	startSeq int64
	stopSeq  int64
}

func (f *fakeDomain) Name() string { return f.name }

func (f *fakeDomain) Start(_ context.Context, _ Deps) error {
	if f.started != nil {
		f.startSeq = f.started.Add(1)
	}
	return f.startErr
}

func (f *fakeDomain) Stop() error {
	if f.stopped != nil {
		f.stopSeq = f.stopped.Add(1)
	}
	return nil
}

func TestRegistry_RegisterAndNamesPreserveOrder(t *testing.T) {
	r := NewRegistry()
	r.Register(&fakeDomain{name: "alpha"})
	r.Register(&fakeDomain{name: "beta"})
	r.Register(&fakeDomain{name: "gamma"})

	got := r.Names()
	want := []string{"alpha", "beta", "gamma"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Names() = %v, want %v", got, want)
	}
}

func TestRegistry_StartAllInRegistrationOrder(t *testing.T) {
	var counter atomic.Int64
	r := NewRegistry()

	a := &fakeDomain{name: "a", started: &counter}
	b := &fakeDomain{name: "b", started: &counter}
	c := &fakeDomain{name: "c", started: &counter}
	r.Register(a)
	r.Register(b)
	r.Register(c)

	if err := r.StartAll(context.Background(), Deps{}); err != nil {
		t.Fatalf("StartAll: %v", err)
	}
	if a.startSeq != 1 || b.startSeq != 2 || c.startSeq != 3 {
		t.Errorf("start order seq = (%d,%d,%d), want (1,2,3)", a.startSeq, b.startSeq, c.startSeq)
	}
}

func TestRegistry_StartAll_PropagatesErrorWithDomainName(t *testing.T) {
	r := NewRegistry()
	boom := errors.New("boom")
	r.Register(&fakeDomain{name: "ok"})
	r.Register(&fakeDomain{name: "broken", startErr: boom})

	err := r.StartAll(context.Background(), Deps{})
	if err == nil {
		t.Fatal("expected error from StartAll, got nil")
	}
	if !errors.Is(err, boom) {
		t.Errorf("error chain should wrap boom, got %v", err)
	}
	// The wrapping message should identify which domain failed; this is the
	// kind of detail an operator needs from a startup-failure log line.
	if got := err.Error(); got == "" || !contains(got, "broken") {
		t.Errorf("error %q should mention the failing domain name", got)
	}
}

func TestRegistry_StopAllReverseOrder(t *testing.T) {
	var counter atomic.Int64
	r := NewRegistry()

	a := &fakeDomain{name: "a", stopped: &counter}
	b := &fakeDomain{name: "b", stopped: &counter}
	c := &fakeDomain{name: "c", stopped: &counter}
	r.Register(a)
	r.Register(b)
	r.Register(c)

	r.StopAll()
	// Reverse order: c stops first, then b, then a.
	if c.stopSeq != 1 || b.stopSeq != 2 || a.stopSeq != 3 {
		t.Errorf("stop order seq = a=%d b=%d c=%d, want a=3 b=2 c=1",
			a.stopSeq, b.stopSeq, c.stopSeq)
	}
}

func TestRegistry_NamesEmptyBeforeRegister(t *testing.T) {
	r := NewRegistry()
	if got := r.Names(); len(got) != 0 {
		t.Errorf("Names() before register = %v, want empty", got)
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
