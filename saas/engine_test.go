package saas

import (
	"strings"
	"testing"
)

func TestMultiEngine_Generation(t *testing.T) {
	eng, err := NewMultiEngine(0, 7, DefaultCustomBitConfig())
	if err != nil {
		t.Fatalf("failed to create multi engine: %v", err)
	}

	formats := []IDFormat{FormatSnowflake, FormatULID, FormatUUIDv7, FormatTSID, FormatCustom}

	for _, fmtName := range formats {
		t.Run(string(fmtName), func(t *testing.T) {
			id, err := eng.Generate(fmtName, "test_")
			if err != nil {
				t.Fatalf("failed to generate %s ID: %v", fmtName, err)
			}
			if !strings.HasPrefix(id, "test_") {
				t.Errorf("expected prefix 'test_', got ID: %s", id)
			}
		})
	}
}

func TestMultiEngine_Batch(t *testing.T) {
	eng, err := NewMultiEngine(0, 1, DefaultCustomBitConfig())
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	ids, err := eng.GenerateBatch(FormatSnowflake, "", 500)
	if err != nil {
		t.Fatalf("GenerateBatch failed: %v", err)
	}
	if len(ids) != 500 {
		t.Fatalf("expected 500 IDs, got %d", len(ids))
	}

	// Verify monotonicity & uniqueness
	seen := make(map[string]bool)
	for _, id := range ids {
		if seen[id] {
			t.Fatalf("duplicate ID detected in batch: %s", id)
		}
		seen[id] = true
	}
}

func TestMultiEngine_Decode(t *testing.T) {
	eng, err := NewMultiEngine(0, 12, DefaultCustomBitConfig())
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	sfID, _ := eng.Generate(FormatSnowflake, "")
	dec, err := eng.DecodeID(sfID)
	if err != nil {
		t.Fatalf("failed to decode snowflake ID: %v", err)
	}
	if dec.Format != "snowflake" {
		t.Errorf("expected format 'snowflake', got '%s'", dec.Format)
	}
	if dec.MachineID != 12 {
		t.Errorf("expected machine ID 12, got %d", dec.MachineID)
	}
}
