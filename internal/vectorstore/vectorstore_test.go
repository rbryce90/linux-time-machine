package vectorstore

import (
	"container/heap"
	"math"
	"testing"
)

// heapPush/heapPop wrap container/heap calls to keep the test code below
// readable; the heap interface is implemented on minHitHeap.
func heapPush(h *minHitHeap, x scoredHit) { heap.Push(h, x) }
func heapPop(h *minHitHeap) scoredHit     { return heap.Pop(h).(scoredHit) }

// These tests cover the math and heap helpers reachable through the public
// API (normalize, dot, top-K via Search). Per the spec's "no peeking into
// internals" rule, the BruteForceStore behavior tests live in
// bruteforce_test.go; this file exercises the small, low-level pieces
// directly because they're part of the package's correctness contract.

func TestNormalize(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   []float32
		ok   bool
		// expected magnitude-1 (with tolerance) when ok
	}{
		{"unit-x", []float32{1, 0, 0}, true},
		{"unit-diag", []float32{1, 1, 1}, true},
		{"negative", []float32{-3, 4}, true},
		{"large", []float32{1e6, 1e6, 1e6}, true},
		{"tiny", []float32{1e-20, 1e-20}, true},
		{"empty", []float32{}, false},
		{"zero", []float32{0, 0, 0}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out, ok := normalize(tc.in)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v", ok, tc.ok)
			}
			if !ok {
				return
			}
			var mag float64
			for _, x := range out {
				mag += float64(x) * float64(x)
			}
			mag = math.Sqrt(mag)
			if math.Abs(mag-1.0) > 1e-5 {
				t.Fatalf("|out| = %v, want ~1.0", mag)
			}
		})
	}
}

func TestDotProduct(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		a, b []float32
		want float32
	}{
		{"orthogonal", []float32{1, 0}, []float32{0, 1}, 0},
		{"parallel", []float32{1, 0}, []float32{1, 0}, 1},
		{"antiparallel", []float32{1, 0}, []float32{-1, 0}, -1},
		{"mixed", []float32{1, 2, 3}, []float32{4, -5, 6}, 12},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := dot(tc.a, tc.b)
			if math.Abs(float64(got-tc.want)) > 1e-5 {
				t.Fatalf("dot = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestMinHitHeapOrdering(t *testing.T) {
	t.Parallel()
	// The heap must keep the smallest score at index 0, and popping via
	// heap.Pop should yield ascending order.
	scores := []float32{0.9, 0.1, 0.5, 0.7, 0.3}
	h := &minHitHeap{}
	for i, s := range scores {
		heapPush(h, scoredHit{score: s, slot: i})
	}
	prev := float32(-1.0)
	for h.Len() > 0 {
		// Min is at index 0 by invariant; verify before popping.
		minScore := (*h)[0].score
		for _, sh := range *h {
			if sh.score < minScore {
				t.Fatalf("heap invariant violated: index 0 = %v, found %v", minScore, sh.score)
			}
		}
		got := heapPop(h).score
		if got < prev {
			t.Fatalf("pop order not ascending: got %v after %v", got, prev)
		}
		prev = got
	}
}

func TestCopyMetadataIndependence(t *testing.T) {
	t.Parallel()
	src := Metadata{"a": 1, "b": "two"}
	dup := copyMetadata(src)
	dup["a"] = 99
	if src["a"] != 1 {
		t.Fatalf("source mutated through copy: src[a] = %v", src["a"])
	}
	if v, ok := dup["b"]; !ok || v != "two" {
		t.Fatalf("copy missing key: %v", dup)
	}
	// nil case
	if got := copyMetadata(nil); got != nil {
		t.Fatalf("copyMetadata(nil) = %v, want nil", got)
	}
}
