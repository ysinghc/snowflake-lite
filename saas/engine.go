package saas

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ysinghc/snowflake-lite/snowflake"
)

// IDFormat represents the supported ID scheme.
type IDFormat string

const (
	FormatSnowflake IDFormat = "snowflake" // 64-bit Twitter Snowflake
	FormatULID      IDFormat = "ulid"      // 128-bit Lexicographically sortable Base32
	FormatUUIDv7    IDFormat = "uuidv7"    // RFC 9562 Time-Ordered UUIDv7
	FormatTSID      IDFormat = "tsid"      // 64-bit Time-Sorted ID
	FormatCustom    IDFormat = "custom"    // Enterprise Custom Bit-Partitioned ID
)

// DecodedID holds structured field data extracted from any decoded ID.
type DecodedID struct {
	Format           string    `json:"format"`
	Raw              string    `json:"raw"`
	Timestamp        time.Time `json:"timestamp"`
	MillisSinceEpoch uint64    `json:"millisSinceEpoch"`
	DatacenterID     uint64    `json:"datacenterID,omitempty"`
	MachineID        uint64    `json:"machineID"`
	Sequence         uint64    `json:"sequence"`
	BitDetails       string    `json:"bitDetails,omitempty"`
}

// CustomBitConfig defines custom enterprise bit partitions.
type CustomBitConfig struct {
	TimestampBits uint8 `json:"timestampBits"` // e.g., 41 bits
	DatacenterBits uint8 `json:"datacenterBits"`// e.g., 5 bits
	WorkerBits    uint8 `json:"workerBits"`    // e.g., 5 bits
	SequenceBits  uint8 `json:"sequenceBits"`  // e.g., 12 bits
	Epoch         int64 `json:"epoch"`         // Custom Unix epoch in ms
}

// DefaultCustomBitConfig returns standard Twitter 41/0/10/12 partition setup.
func DefaultCustomBitConfig() CustomBitConfig {
	return CustomBitConfig{
		TimestampBits: 41,
		DatacenterBits: 0,
		WorkerBits:    10,
		SequenceBits:  12,
		Epoch:         snowflake.Epoch,
	}
}

// MultiEngine orchestrates distributed ID generation across multiple standards.
type MultiEngine struct {
	snowflakeGen *snowflake.Generator
	datacenterID uint64
	machineID    uint64
	customConfig CustomBitConfig

	// State for lock-free custom bit generator
	customState atomic.Uint64

	// ULID monotonic state
	ulidMu       sync.Mutex
	lastULIDTime uint64
	lastULIDRand [10]byte
}

// NewMultiEngine initializes multi-format generator engine for node machineID.
func NewMultiEngine(datacenterID, machineID uint64, customCfg CustomBitConfig) (*MultiEngine, error) {
	if customCfg.TimestampBits == 0 {
		customCfg = DefaultCustomBitConfig()
	}
	sfGen, err := snowflake.New(int64(machineID))
	if err != nil {
		return nil, fmt.Errorf("engine: failed to create snowflake generator: %w", err)
	}

	return &MultiEngine{
		snowflakeGen: sfGen,
		datacenterID: datacenterID,
		machineID:    machineID,
		customConfig: customCfg,
	}, nil
}

// Generate mints an ID in requested format, optionally with type prefix (e.g. "usr_").
func (m *MultiEngine) Generate(format IDFormat, prefix string) (string, error) {
	var idStr string
	switch strings.ToLower(string(format)) {
	case string(FormatSnowflake):
		idStr = m.snowflakeGen.Next().String()

	case string(FormatULID):
		id, err := m.nextULID()
		if err != nil {
			return "", err
		}
		idStr = id

	case string(FormatUUIDv7):
		idStr = m.nextUUIDv7()

	case string(FormatTSID):
		idStr = m.nextTSID()

	case string(FormatCustom):
		idStr = m.nextCustomBitID()

	default:
		// Fallback to Snowflake
		idStr = m.snowflakeGen.Next().String()
	}

	if prefix != "" {
		return prefix + idStr, nil
	}
	return idStr, nil
}

// GenerateBatch mints N IDs efficiently.
func (m *MultiEngine) GenerateBatch(format IDFormat, prefix string, count int) ([]string, error) {
	if count <= 0 {
		count = 1
	}
	if count > 100000 {
		return nil, fmt.Errorf("batch count exceeds max limit of 100,000")
	}

	results := make([]string, count)
	for i := 0; i < count; i++ {
		id, err := m.Generate(format, prefix)
		if err != nil {
			return nil, err
		}
		results[i] = id
	}
	return results, nil
}

// --- ULID Generator Implementation ---
const crockfordBase32 = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

func (m *MultiEngine) nextULID() (string, error) {
	m.ulidMu.Lock()
	defer m.ulidMu.Unlock()

	now := uint64(time.Now().UnixMilli())
	var randBytes [10]byte

	if now == m.lastULIDTime {
		// Increment 80-bit random bytes monotonically
		for i := 9; i >= 0; i-- {
			m.lastULIDRand[i]++
			if m.lastULIDRand[i] != 0 {
				break
			}
		}
		randBytes = m.lastULIDRand
	} else {
		m.lastULIDTime = now
		if _, err := rand.Read(randBytes[:]); err != nil {
			return "", fmt.Errorf("ulid entropy error: %w", err)
		}
		m.lastULIDRand = randBytes
	}

	// Pack 48-bit timestamp + 80-bit entropy (16 bytes total)
	var raw [16]byte
	raw[0] = byte(now >> 40)
	raw[1] = byte(now >> 32)
	raw[2] = byte(now >> 24)
	raw[3] = byte(now >> 16)
	raw[4] = byte(now >> 8)
	raw[5] = byte(now)
	copy(raw[6:], randBytes[:])

	return encodeCrockford32(raw), nil
}

func encodeCrockford32(b [16]byte) string {
	var dst [26]byte
	// 128 bits into 26 chars (5 bits per char)
	dst[0] = crockfordBase32[(b[0]&0xE0)>>5]
	dst[1] = crockfordBase32[(b[0]&0x1F)]
	dst[2] = crockfordBase32[(b[1]&0xF8)>>3]
	dst[3] = crockfordBase32[((b[1]&0x07)<<2)|((b[2]&0xC0)>>6)]
	dst[4] = crockfordBase32[(b[2]&0x3E)>>1]
	dst[5] = crockfordBase32[((b[2]&0x01)<<4)|((b[3]&0xF0)>>4)]
	dst[6] = crockfordBase32[((b[3]&0x0F)<<1)|((b[4]&0x80)>>7)]
	dst[7] = crockfordBase32[(b[4]&0x7C)>>2]
	dst[8] = crockfordBase32[((b[4]&0x03)<<3)|((b[5]&0xE0)>>5)]
	dst[9] = crockfordBase32[(b[5]&0x1F)]

	dst[10] = crockfordBase32[(b[6]&0xF8)>>3]
	dst[11] = crockfordBase32[((b[6]&0x07)<<2)|((b[7]&0xC0)>>6)]
	dst[12] = crockfordBase32[(b[7]&0x3E)>>1]
	dst[13] = crockfordBase32[((b[7]&0x01)<<4)|((b[8]&0xF0)>>4)]
	dst[14] = crockfordBase32[((b[8]&0x0F)<<1)|((b[9]&0x80)>>7)]
	dst[15] = crockfordBase32[(b[9]&0x7C)>>2]
	dst[16] = crockfordBase32[((b[9]&0x03)<<3)|((b[10]&0xE0)>>5)]
	dst[17] = crockfordBase32[(b[10]&0x1F)]
	dst[18] = crockfordBase32[(b[11]&0xF8)>>3]
	dst[19] = crockfordBase32[((b[11]&0x07)<<2)|((b[12]&0xC0)>>6)]
	dst[20] = crockfordBase32[(b[12]&0x3E)>>1]
	dst[21] = crockfordBase32[((b[12]&0x01)<<4)|((b[13]&0xF0)>>4)]
	dst[22] = crockfordBase32[((b[13]&0x0F)<<1)|((b[14]&0x80)>>7)]
	dst[23] = crockfordBase32[(b[14]&0x7C)>>2]
	dst[24] = crockfordBase32[((b[14]&0x03)<<3)|((b[15]&0xE0)>>5)]
	dst[25] = crockfordBase32[(b[15]&0x1F)]
	return string(dst[:])
}

// --- UUIDv7 Generator Implementation ---
func (m *MultiEngine) nextUUIDv7() string {
	now := uint64(time.Now().UnixMilli())
	var b [16]byte
	_, _ = rand.Read(b[6:])

	// 48-bit timestamp
	b[0] = byte(now >> 40)
	b[1] = byte(now >> 32)
	b[2] = byte(now >> 24)
	b[3] = byte(now >> 16)
	b[4] = byte(now >> 8)
	b[5] = byte(now)

	// UUIDv7 version bits (0111) in byte 6 high nibble
	b[6] = (b[6] & 0x0F) | 0x70
	// UUID variant bits (10xx) in byte 8 high 2 bits
	b[8] = (b[8] & 0x3F) | 0x80

	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// --- TSID Generator Implementation ---
func (m *MultiEngine) nextTSID() string {
	id := m.snowflakeGen.Next()
	return encodeBase62(uint64(id))
}

const base62Chars = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

func encodeBase62(num uint64) string {
	if num == 0 {
		return "0"
	}
	var buf [13]byte
	i := len(buf)
	for num > 0 {
		i--
		buf[i] = base62Chars[num%62]
		num /= 62
	}
	return string(buf[i:])
}

// --- Custom Enterprise Bit Partition Generator ---
func (m *MultiEngine) nextCustomBitID() string {
	cfg := m.customConfig
	seqShift := uint64(0)
	workerShift := uint64(cfg.SequenceBits)
	dcShift := uint64(cfg.SequenceBits + cfg.WorkerBits)
	tsShift := uint64(cfg.SequenceBits + cfg.WorkerBits + cfg.DatacenterBits)

	seqMask := uint64((1 << cfg.SequenceBits) - 1)
	dcMask := uint64((1 << cfg.DatacenterBits) - 1)
	workerMask := uint64((1 << cfg.WorkerBits) - 1)

	for {
		old := m.customState.Load()
		lastTS := old >> cfg.SequenceBits
		lastSeq := old & seqMask

		nowMs := time.Now().UnixMilli() - cfg.Epoch
		if nowMs < 0 {
			nowMs = 0
		}
		nowTS := uint64(nowMs)

		var ts, seq uint64
		if nowTS > lastTS {
			ts, seq = nowTS, 0
		} else {
			ts, seq = lastTS, lastSeq+1
			if seq > seqMask {
				ts, seq = lastTS+1, 0
			}
		}

		nextState := (ts << cfg.SequenceBits) | seq
		if m.customState.CompareAndSwap(old, nextState) {
			val := (ts << tsShift) |
				((m.datacenterID & dcMask) << dcShift) |
				((m.machineID & workerMask) << workerShift) |
				(seq << seqShift)
			return strconv.FormatUint(val, 10)
		}
	}
}

// DecodeID parses an ID string and returns detailed field breakdown.
func (m *MultiEngine) DecodeID(raw string) (DecodedID, error) {
	// Strip prefix if present (e.g. "usr_12345678")
	parts := strings.Split(raw, "_")
	idStr := parts[len(parts)-1]

	// Try numeric Snowflake decode
	if num, err := strconv.ParseInt(idStr, 10, 64); err == nil && num > 0 {
		sfDec := snowflake.Decode(snowflake.ID(num))
		bStr := fmt.Sprintf("%064b", uint64(num))
		return DecodedID{
			Format:           "snowflake",
			Raw:              raw,
			Timestamp:        sfDec.Timestamp,
			MillisSinceEpoch: sfDec.MillisSinceEpoch,
			MachineID:        sfDec.MachineID,
			Sequence:         sfDec.Sequence,
			BitDetails:       fmt.Sprintf("ts: %s | machine: %s | seq: %s", bStr[1:42], bStr[42:52], bStr[52:64]),
		}, nil
	}

	// Try UUIDv7 decode
	if len(idStr) == 36 && strings.Count(idStr, "-") == 4 {
		hexStr := strings.ReplaceAll(idStr, "-", "")
		if len(hexStr) == 32 {
			if tsBytes, err := parseHex(hexStr[0:12]); err == nil {
				ms := uint64(tsBytes)
				t := time.UnixMilli(int64(ms)).UTC()
				return DecodedID{
					Format:           "uuidv7",
					Raw:              raw,
					Timestamp:        t,
					MillisSinceEpoch: ms,
					MachineID:        m.machineID,
					Sequence:         0,
					BitDetails:       fmt.Sprintf("Version: 7 | 48-bit timestamp (%d ms)", ms),
				}, nil
			}
		}
	}

	// Try ULID decode (26 chars)
	if len(idStr) == 26 {
		ms := decodeULIDTimestamp(idStr)
		if ms > 0 {
			t := time.UnixMilli(int64(ms)).UTC()
			return DecodedID{
				Format:           "ulid",
				Raw:              raw,
				Timestamp:        t,
				MillisSinceEpoch: ms,
				MachineID:        m.machineID,
				Sequence:         0,
				BitDetails:       fmt.Sprintf("Crockford Base32 128-bit sortable | Timestamp (%s)", t.Format(time.RFC3339)),
			}, nil
		}
	}

	return DecodedID{
		Format:    "unknown",
		Raw:       raw,
		Timestamp: time.Now().UTC(),
	}, nil
}

func parseHex(hex string) (uint64, error) {
	var b [8]byte
	if len(hex) < 12 {
		return 0, fmt.Errorf("invalid hex length")
	}
	hexFull := "0000" + hex
	for i := 0; i < 8; i++ {
		_, err := fmt.Sscanf(hexFull[i*2:i*2+2], "%02x", &b[i])
		if err != nil {
			return 0, err
		}
	}
	return binary.BigEndian.Uint64(b[:]), nil
}

func decodeULIDTimestamp(s string) uint64 {
	if len(s) < 10 {
		return 0
	}
	var val uint64
	for i := 0; i < 10; i++ {
		idx := strings.IndexByte(crockfordBase32, s[i])
		if idx < 0 {
			return 0
		}
		val = (val << 5) | uint64(idx)
	}
	return val
}
