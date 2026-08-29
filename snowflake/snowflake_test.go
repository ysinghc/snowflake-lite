package snowflake

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func newTestGen(t *testing.T, machine int64, now func() int64) *Generator {
	t.Helper()
	g, err := New(machine)
	if err != nil {
		t.Fatalf("New(%d): %v", machine, err)
	}
	g.now = now
	return g
}

// TestUniquenessConcurrent hammers a single generator from many goroutines and
// asserts every ID is unique — the core guarantee of the CAS design.
func TestUniquenessConcurrent(t *testing.T) {
	g := MustNew(42)
	const goroutines, perG = 16, 50_000

	results := make([][]ID, goroutines)
	var wg sync.WaitGroup
	for i := range goroutines {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ids := make([]ID, perG)
			for j := range ids {
				ids[j] = g.Next()
			}
			results[i] = ids
		}(i)
	}
	wg.Wait()

	seen := make(map[ID]struct{}, goroutines*perG)
	for _, ids := range results {
		for _, id := range ids {
			if _, dup := seen[id]; dup {
				t.Fatalf("duplicate ID: %d", id)
			}
			seen[id] = struct{}{}
			if p := Decode(id); p.MachineID != 42 {
				t.Fatalf("machine ID: got %d want 42", p.MachineID)
			}
		}
	}
	if want := goroutines * perG; len(seen) != want {
		t.Fatalf("unique count: got %d want %d", len(seen), want)
	}
}

// TestMonotonic checks IDs from one goroutine are strictly increasing.
func TestMonotonic(t *testing.T) {
	g := MustNew(1)
	prev := ID(-1)
	for i := range 200_000 {
		id := g.Next()
		if id <= prev {
			t.Fatalf("not strictly increasing at %d: %d <= %d", i, id, prev)
		}
		prev = id
	}
}

// TestClockBackwardMonotonic is the headline property: even when the wall clock
// jumps backward mid-run (an NTP step), IDs remain unique and strictly
// ascending — no sleep, no error.
func TestClockBackwardMonotonic(t *testing.T) {
	base := Epoch + 10_000
	var calls atomic.Int64
	g := newTestGen(t, 3, func() int64 {
		if calls.Add(1) > 100 {
			return base - 5_000 // clock steps 5s into the past and stays there
		}
		return base
	})

	const n = 5000
	prev := ID(-1)
	seen := make(map[ID]struct{}, n)
	for i := range n {
		id := g.Next()
		if id <= prev {
			t.Fatalf("id regressed at %d: %d <= %d", i, id, prev)
		}
		if _, dup := seen[id]; dup {
			t.Fatalf("duplicate id after clock jump: %d", id)
		}
		seen[id] = struct{}{}
		prev = id
	}
}

// TestSequenceOverflowTicksClock freezes the wall clock and mints far more than
// 4096 IDs, forcing the logical clock to advance on its own.
func TestSequenceOverflowTicksClock(t *testing.T) {
	fixed := Epoch + 50_000
	g := newTestGen(t, 1, func() int64 { return fixed })

	const n = MaxSequence*3 + 7 // spills across four logical milliseconds
	prev := ID(-1)
	seen := make(map[ID]struct{}, n)
	for i := range n {
		id := g.Next()
		if id <= prev {
			t.Fatalf("id regressed at %d: %d <= %d", i, id, prev)
		}
		if _, dup := seen[id]; dup {
			t.Fatalf("duplicate id: %d", id)
		}
		seen[id] = struct{}{}
		prev = id
	}

	got := Decode(prev).MillisSinceEpoch
	wantAtLeast := uint64(fixed-Epoch) + uint64(n/(MaxSequence+1))
	if got < wantAtLeast {
		t.Fatalf("logical clock did not advance past frozen wall clock: got %d want >= %d", got, wantAtLeast)
	}
}

// TestDecodeRoundTrip verifies decode is the exact inverse of compose.
func TestDecodeRoundTrip(t *testing.T) {
	g := MustNew(511)
	for range 1000 {
		id := g.Next()
		p := Decode(id)
		if p.MachineID != 511 {
			t.Fatalf("machine: got %d want 511", p.MachineID)
		}
		if p.Sequence > MaxSequence {
			t.Fatalf("sequence out of range: %d", p.Sequence)
		}
		recomposed := ID(p.MillisSinceEpoch<<timestampShift | p.MachineID<<machineShift | p.Sequence)
		if recomposed != id {
			t.Fatalf("round trip: got %d want %d", recomposed, id)
		}
	}
}

// TestTimestampReflectsWallClock confirms the encoded time tracks real time.
func TestTimestampReflectsWallClock(t *testing.T) {
	g := MustNew(0)
	before := time.Now().Add(-2 * time.Second)
	after := time.Now().Add(2 * time.Second)
	got := g.Next().Time()
	if got.Before(before) || got.After(after) {
		t.Fatalf("encoded time %v not within [%v, %v]", got, before, after)
	}
}

func TestMachineIDBounds(t *testing.T) {
	for _, bad := range []int64{-1, MaxMachineID + 1, 5000} {
		if _, err := New(bad); err == nil {
			t.Fatalf("expected error for machine ID %d", bad)
		}
	}
	for _, ok := range []int64{0, 1, MaxMachineID} {
		g, err := New(ok)
		if err != nil {
			t.Fatalf("unexpected error for machine ID %d: %v", ok, err)
		}
		if g.MachineID() != ok {
			t.Fatalf("MachineID(): got %d want %d", g.MachineID(), ok)
		}
	}
}

func BenchmarkNextSingle(b *testing.B) {
	g := MustNew(1)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = g.Next()
	}
}

func BenchmarkNextParallel(b *testing.B) {
	g := MustNew(1)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = g.Next()
		}
	})
}

// mutexGen mirrors the same logic behind a mutex, so the benchmark can show the
// cost the lock-free design avoids. Used only in tests.
type mutexGen struct {
	mu        sync.Mutex
	machineID uint64
	lastTS    uint64
	seq       uint64
}

func (m *mutexGen) Next() ID {
	m.mu.Lock()
	now := uint64(time.Now().UnixMilli() - Epoch)
	if now > m.lastTS {
		m.lastTS, m.seq = now, 0
	} else {
		m.seq++
		if m.seq > seqMask {
			m.lastTS++
			m.seq = 0
		}
	}
	id := ID(m.lastTS<<timestampShift | m.machineID<<machineShift | m.seq)
	m.mu.Unlock()
	return id
}

func BenchmarkNextMutexParallel(b *testing.B) {
	m := &mutexGen{machineID: 1}
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = m.Next()
		}
	})
}
