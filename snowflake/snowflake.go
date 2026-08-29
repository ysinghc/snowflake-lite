// Package snowflake generates 64-bit, time-sortable, collision-free unique IDs
// with no central coordination — a lock-free take on Twitter's Snowflake.
//
// # Layout (63 usable bits, sign bit unused so every ID is a positive int64)
//
//		| 1 unused | 41-bit timestamp | 10-bit machine | 12-bit sequence |
//		   bit 63     bits 62..22        bits 21..12       bits 11..0
//
//	  - timestamp: milliseconds since a custom epoch (2024-01-01). 41 bits ≈ 69
//	    years of headroom (until ~2093).
//	  - machine:   0..1023, so up to 1024 independent generators.
//	  - sequence:  0..4095, so 4096 IDs per millisecond per machine
//	    (≈ 4.096M IDs/sec/machine before the logical clock leads wall time).
//
// # Why lock-free
//
// The textbook Snowflake serializes generation behind a mutex and *sleeps or
// errors* when the wall clock steps backward. This package instead packs the
// mutable state — (timestamp<<12 | sequence) — into a single atomic uint64 and
// mints each ID with one CompareAndSwap. There is no lock, and one branch
// handles both "same millisecond" and "clock moved backward": we never regress,
// we advance the sequence, and on overflow we tick a *logical* clock forward by
// one millisecond. The generator never sleeps and never returns an error; IDs
// are always unique and strictly ascending, even across an NTP step back.
package snowflake

import (
	"fmt"
	"hash/fnv"
	"os"
	"strconv"
	"sync/atomic"
	"time"
)

// Epoch is the custom origin for timestamps: 2024-01-01T00:00:00Z, in Unix
// milliseconds. Using a recent epoch (instead of 1970) maximizes the 41-bit
// window — it lasts ~69 years from here.
const Epoch int64 = 1704067200000

const (
	timestampBits = 41
	machineBits   = 10
	sequenceBits  = 12

	// Field maxima, exported so callers and tests can reason about limits.
	MaxMachineID = (1 << machineBits) - 1  // 1023
	MaxSequence  = (1 << sequenceBits) - 1 // 4095

	machineShift   = sequenceBits               // 12
	timestampShift = sequenceBits + machineBits // 22

	seqMask     uint64 = (1 << sequenceBits) - 1 // 0xFFF
	machineMask uint64 = (1 << machineBits) - 1  // 0x3FF
)

// Compile-time guarantee that the three fields fill exactly the 63 usable bits
// (the sign bit is the unused one). If a layout constant is ever changed
// inconsistently, the constant index goes out of range and the build fails.
var _ = [1]struct{}{}[63-(timestampBits+machineBits+sequenceBits)]

// ID is a generated Snowflake identifier. It is an int64 so it maps cleanly to
// SQL BIGINT columns; the top bit is always zero, so the value is non-negative.
type ID int64

// Int64 returns the raw integer value.
func (id ID) Int64() int64 { return int64(id) }

// String renders the ID in base 10.
//
// Note for JS/JSON consumers: a Snowflake exceeds 2^53, so it MUST be carried
// as a string across a float64 boundary (JSON numbers, JS `number`) or the low
// bits are silently corrupted. Prefer this string form on the wire.
func (id ID) String() string { return strconv.FormatInt(int64(id), 10) }

// Time returns the wall-clock instant encoded in the ID.
func (id ID) Time() time.Time { return Decode(id).Timestamp }

// Decode splits the ID back into its fields.
func (id ID) Decode() Parts { return Decode(id) }

// Parts is the decoded view of an ID.
type Parts struct {
	ID               ID        `json:"-"`
	Timestamp        time.Time `json:"timestamp"`
	MillisSinceEpoch uint64    `json:"millisSinceEpoch"`
	MachineID        uint64    `json:"machineID"`
	Sequence         uint64    `json:"sequence"`
}

// Decode extracts the timestamp, machine ID, and sequence from an ID.
func Decode(id ID) Parts {
	u := uint64(id)
	ms := u >> timestampShift
	return Parts{
		ID:               id,
		Timestamp:        time.UnixMilli(int64(ms) + Epoch).UTC(),
		MillisSinceEpoch: ms,
		MachineID:        (u >> machineShift) & machineMask,
		Sequence:         u & seqMask,
	}
}

// Generator mints IDs for a single machine. It is safe for concurrent use by
// any number of goroutines; the zero value is not usable — construct one with
// New, MustNew, or NewFromEnv.
type Generator struct {
	machineID uint64
	// state holds (timestamp<<sequenceBits | sequence) as one word so the whole
	// generation step is a single atomic CompareAndSwap.
	state atomic.Uint64
	// now returns the current time in Unix milliseconds. It is a field so tests
	// can simulate clock skew; production always uses the wall clock.
	now func() int64
}

// New returns a Generator for the given machine ID, which must be in
// [0, MaxMachineID].
func New(machineID int64) (*Generator, error) {
	if machineID < 0 || machineID > MaxMachineID {
		return nil, fmt.Errorf("snowflake: machine ID %d out of range [0, %d]", machineID, MaxMachineID)
	}
	g := &Generator{machineID: uint64(machineID), now: unixMillis}
	return g, nil
}

// MustNew is like New but panics on an invalid machine ID.
func MustNew(machineID int64) *Generator {
	g, err := New(machineID)
	if err != nil {
		panic(err)
	}
	return g
}

// NewFromEnv builds a Generator using the MACHINE_ID environment variable. If
// MACHINE_ID is unset it derives an ID from the hostname.
//
// The hostname fallback is a convenience, not a guarantee: two hosts can hash
// to the same 10-bit value. Assign MACHINE_ID explicitly for real distributed
// deployments (0..1023, unique per process).
func NewFromEnv() (*Generator, error) {
	if s := os.Getenv("MACHINE_ID"); s != "" {
		id, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("snowflake: invalid MACHINE_ID %q: %w", s, err)
		}
		return New(id)
	}
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "localhost"
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(host))
	return New(int64(h.Sum32() % (MaxMachineID + 1)))
}

// MachineID reports the machine ID embedded in every ID this Generator mints.
func (g *Generator) MachineID() int64 { return int64(g.machineID) }

// Next returns the next unique, monotonically increasing ID. It never blocks
// and never fails.
func (g *Generator) Next() ID {
	for {
		old := g.state.Load()
		lastTS := old >> sequenceBits
		lastSeq := old & seqMask

		now := g.sinceEpoch()

		var ts, seq uint64
		if now > lastTS {
			// Fresh millisecond: start its sequence at 0.
			ts, seq = now, 0
		} else {
			// Same millisecond, or the wall clock moved backward. Never
			// regress — hold the logical timestamp and advance the sequence.
			ts, seq = lastTS, lastSeq+1
			if seq > seqMask {
				// This millisecond is full: tick the logical clock forward by
				// 1ms and restart the sequence. IDs stay strictly ascending
				// even if we briefly lead the wall clock.
				ts, seq = lastTS+1, 0
			}
		}

		next := ts<<sequenceBits | seq
		if g.state.CompareAndSwap(old, next) {
			return ID(ts<<timestampShift | g.machineID<<machineShift | seq)
		}
		// Another goroutine advanced the state first; observe it and retry.
	}
}

// NextN returns n IDs. Convenience wrapper around Next.
func (g *Generator) NextN(n int) []ID {
	ids := make([]ID, n)
	for i := range ids {
		ids[i] = g.Next()
	}
	return ids
}

// sinceEpoch is the current time in milliseconds since Epoch, clamped at 0 for
// the pathological case of a clock set before 2024.
func (g *Generator) sinceEpoch() uint64 {
	ms := g.now()
	if ms < Epoch {
		return 0
	}
	return uint64(ms - Epoch)
}

func unixMillis() int64 { return time.Now().UnixMilli() }
