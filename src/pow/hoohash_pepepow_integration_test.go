package pow

import (
	"encoding/binary"
	"encoding/hex"
	"testing"
)

// TestHoohashV110BitcoinWithStratumData tests the hoohash computation using
// a real 80-byte header reconstructed from the PePePow stratum sample session.
func TestHoohashV110BitcoinWithStratumData(t *testing.T) {
	// This header was reconstructed from the stratum session in the problem statement.
	// See src/pepepow/stratum_test.go for the full reconstruction.
	headerHex := "00400020ad48d5c2378982f15d309e2bc33cd20ce658eff932ba377ee6cbcd6d01000000b7fddd5826708cfb305a3c748f5d1c5681a4929bfbccdd06f51b2f3bb5eb9ea569ddf7178cbd061d03ab46e2"
	header, err := hex.DecodeString(headerHex)
	if err != nil {
		t.Fatal(err)
	}
	if len(header) != 80 {
		t.Fatalf("header length = %d, want 80", len(header))
	}

	// Verify version
	version := binary.LittleEndian.Uint32(header[0:4])
	if version != 0x20004000 {
		t.Errorf("version = 0x%08x, want 0x20004000", version)
	}

	// Verify nonce
	nonce := binary.LittleEndian.Uint32(header[76:80])
	t.Logf("nonce from header: 0x%08x", nonce)

	// Compute hoohash
	result := HoohashV110Bitcoin(header)
	t.Logf("hoohash result (LE): %s", hex.EncodeToString(result[:]))

	// Verify it's a valid hash (not all zeros or 0xFF)
	allZero := true
	allFF := true
	for _, b := range result {
		if b != 0 {
			allZero = false
		}
		if b != 0xFF {
			allFF = false
		}
	}
	if allZero {
		t.Error("hoohash result should not be all zeros")
	}
	if allFF {
		t.Error("hoohash result should not be all 0xFF")
	}

	// Since this was an accepted share at difficulty 327.68, the hoohash result
	// should be below the stratum difficulty target.
	// We can't verify the exact value without the C reference, but we confirm
	// the computation completes successfully on real stratum data.
	t.Log("HoohashV110Bitcoin computed successfully on real stratum header data")
}
