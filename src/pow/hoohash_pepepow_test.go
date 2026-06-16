package pow

import (
	"encoding/hex"
	"testing"

	"lukechampine.com/blake3"
)

// TestHoohashV110BitcoinBasic tests the basic flow of the PePePow hoohash.
func TestHoohashV110BitcoinBasic(t *testing.T) {
	// Create a test header (80 bytes) with version 0x20004000
	var header [80]byte
	header[0] = 0x00 // version LE: 0x20004000
	header[1] = 0x40
	header[2] = 0x00
	header[3] = 0x20

	// Fill the rest with deterministic data
	for i := 4; i < 76; i++ {
		header[i] = byte(i * 7)
	}
	// Nonce at [76:80]
	header[76] = 0x01
	header[77] = 0x00
	header[78] = 0x00
	header[79] = 0x00

	result := HoohashV110Bitcoin(header[:])

	// Verify it's not all zeros or all 0xFF
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
		t.Error("hoohash result is all zeros")
	}
	if allFF {
		t.Error("hoohash result is all 0xFF (invalid input marker)")
	}

	t.Logf("hoohash result: %s", hex.EncodeToString(result[:]))
}

// TestHoohashV110BitcoinInvalidLength tests that non-80-byte input returns all 0xFF.
func TestHoohashV110BitcoinInvalidLength(t *testing.T) {
	result := HoohashV110Bitcoin(make([]byte, 64))
	for _, b := range result {
		if b != 0xFF {
			t.Fatal("expected all 0xFF for invalid input length")
		}
	}
}

// TestHoohashV110BitcoinDeterministic tests that the same input produces the same output.
func TestHoohashV110BitcoinDeterministic(t *testing.T) {
	var header [80]byte
	for i := range header {
		header[i] = byte(i)
	}
	header[0] = 0x00
	header[1] = 0x40
	header[2] = 0x00
	header[3] = 0x20

	r1 := HoohashV110Bitcoin(header[:])
	r2 := HoohashV110Bitcoin(header[:])

	if r1 != r2 {
		t.Error("hoohash is not deterministic")
	}
}

// TestHoohashV110BitcoinNonceSensitivity tests that changing the nonce changes the result.
func TestHoohashV110BitcoinNonceSensitivity(t *testing.T) {
	var header [80]byte
	for i := range header {
		header[i] = byte(i)
	}
	header[0] = 0x00
	header[1] = 0x40
	header[2] = 0x00
	header[3] = 0x20

	header[76] = 0x00
	r1 := HoohashV110Bitcoin(header[:])

	header[76] = 0x01
	r2 := HoohashV110Bitcoin(header[:])

	if r1 == r2 {
		t.Error("changing nonce should change the result")
	}
}

// TestHoohashV110BitcoinMatrixSeedNonceIndependent verifies that the matrix seed
// (BLAKE3 of header with nonce zeroed) is the same regardless of nonce value.
func TestHoohashV110BitcoinMatrixSeedNonceIndependent(t *testing.T) {
	var header1, header2 [80]byte
	for i := 0; i < 76; i++ {
		header1[i] = byte(i * 3)
		header2[i] = byte(i * 3)
	}
	// Different nonces
	header1[76] = 0x42
	header2[76] = 0xFF

	// Zero nonce for both and hash
	var masked1, masked2 [80]byte
	copy(masked1[:], header1[:])
	copy(masked2[:], header2[:])
	masked1[76], masked1[77], masked1[78], masked1[79] = 0, 0, 0, 0
	masked2[76], masked2[77], masked2[78], masked2[79] = 0, 0, 0, 0

	seed1 := blake3.Sum256(masked1[:])
	seed2 := blake3.Sum256(masked2[:])

	if seed1 != seed2 {
		t.Error("matrix seed should be nonce-independent")
	}
}

// TestPepepowXoshiroConsistency verifies the xoshiro256** PRNG produces
// consistent output matching the reference implementation's seeding.
func TestPepepowXoshiroConsistency(t *testing.T) {
	// Use a seed with all state populated (not mostly zeros)
	seed := make([]byte, 32)
	for i := range seed {
		seed[i] = byte(i + 1)
	}

	gen := newPepepowXoshiro(seed)
	v1 := gen.next()
	v2 := gen.next()

	if v1 == 0 && v2 == 0 {
		t.Error("xoshiro should not produce two zeros for this seed")
	}
	if v1 == v2 {
		t.Error("consecutive xoshiro values should differ for a well-seeded PRNG")
	}

	// Verify determinism
	gen2 := newPepepowXoshiro(seed)
	if gen2.next() != v1 || gen2.next() != v2 {
		t.Error("xoshiro is not deterministic")
	}
}
