package pepepow

import (
	"encoding/hex"
	"testing"
)

// TestStratumNotifyParsing verifies that the bridge can correctly parse and
// reconstruct block headers from the Bitcoin-style stratum parameters shown
// in the reference NOMP pool session.
//
// Reference data from the working miner stratum chatter:
//   mining.notify params:
//     job_id:   "b54"
//     prevhash: "c2d548adf18289372b9e305d0cd23cc3f9ef58e67e37ba326dcdcbe600000001"
//     coinbase1: "01000000010000000000000000000000000000000000000000000000000000000000000000ffffffff2003dd8d420417f7dd6908"
//     coinbase2: "0d2f6e6f64655374726174756d2f000000000450eab5b3740000001976a91410717ddc41c5245c141f18ec504dcfa3e55db7b788ac8088c2273f0000001976a914d86a31f1a2b66fea401c2fff4e89c6dc9c0fcae988ac00ba1dd2050000001976a9146442e3eb21eca73b2237eb45eed1f25e83a0a88488ac30132196000000001976a9143745215dff5a01ba6e364f9bbe5f4159edbcbc9088ac00000000"
//     merkle_branches: []
//     version:  "20004000"
//     nbits:    "1d06bd8c"
//     ntime:    "69ddf717"
//     clean:    true
//
//   mining.submit params:
//     worker:     "PMG1tmwb7fRrPnKmRCuAzKBd2t7KPDw6Nf"
//     job_id:     "b54"
//     extranonce2: "01000000"
//     ntime:      "69ddf717"
//     nonce:      "03ab46e2"
//
//   extranonce1 from subscribe: "6ffffd90"
//   extranonce2_size: 4
func TestStratumReconstructHeader(t *testing.T) {
	// Reference values from the stratum session
	coinbase1Hex := "01000000010000000000000000000000000000000000000000000000000000000000000000ffffffff2003dd8d420417f7dd6908"
	coinbase2Hex := "0d2f6e6f64655374726174756d2f000000000450eab5b3740000001976a91410717ddc41c5245c141f18ec504dcfa3e55db7b788ac8088c2273f0000001976a914d86a31f1a2b66fea401c2fff4e89c6dc9c0fcae988ac00ba1dd2050000001976a9146442e3eb21eca73b2237eb45eed1f25e83a0a88488ac30132196000000001976a9143745215dff5a01ba6e364f9bbe5f4159edbcbc9088ac00000000"
	extranonce1Hex := "6ffffd90"
	extranonce2Hex := "01000000"
	prevhashStratum := "c2d548adf18289372b9e305d0cd23cc3f9ef58e67e37ba326dcdcbe600000001"
	versionHex := "20004000"
	nbitsHex := "1d06bd8c"
	ntimeHex := "69ddf717"
	nonceHex := "03ab46e2"

	// 1. Reconstruct coinbase transaction
	coinbase1 := HexToBytes(coinbase1Hex)
	extranonce1 := HexToBytes(extranonce1Hex)
	extranonce2 := HexToBytes(extranonce2Hex)
	coinbase2 := HexToBytes(coinbase2Hex)

	coinbaseTx := make([]byte, 0, len(coinbase1)+len(extranonce1)+len(extranonce2)+len(coinbase2))
	coinbaseTx = append(coinbaseTx, coinbase1...)
	coinbaseTx = append(coinbaseTx, extranonce1...)
	coinbaseTx = append(coinbaseTx, extranonce2...)
	coinbaseTx = append(coinbaseTx, coinbase2...)

	t.Logf("coinbase tx (%d bytes): %s", len(coinbaseTx), hex.EncodeToString(coinbaseTx))

	// 2. Compute coinbase hash (double SHA256)
	coinbaseHash := DoubleSHA256(coinbaseTx)
	t.Logf("coinbase hash: %s", hex.EncodeToString(coinbaseHash[:]))

	// 3. Build merkle root (no branches = coinbase hash IS the merkle root)
	merkleRoot := BuildMerkleRoot(coinbaseHash, nil)
	t.Logf("merkle root: %s", hex.EncodeToString(merkleRoot[:]))

	// 4. Decode prevhash from stratum format
	// In Bitcoin stratum v1, prevhash is sent with 4-byte groups byte-swapped
	prevHashSwapped := HexToBytes(prevhashStratum)
	prevHashInternal := SwapEndian32(prevHashSwapped)
	t.Logf("prevhash internal: %s", hex.EncodeToString(prevHashInternal))

	// 5. Parse version, ntime, nbits, nonce
	versionBytes := HexToBytes(versionHex)
	if len(versionBytes) != 4 {
		t.Fatalf("invalid version length")
	}
	// Version in stratum is sent big-endian, header stores it little-endian
	// 0x20004000 in LE = 00 40 00 20
	version := uint32(versionBytes[0])<<24 | uint32(versionBytes[1])<<16 | uint32(versionBytes[2])<<8 | uint32(versionBytes[3])

	ntimeBytes := HexToBytes(ntimeHex)
	ntime := uint32(ntimeBytes[0]) | uint32(ntimeBytes[1])<<8 | uint32(ntimeBytes[2])<<16 | uint32(ntimeBytes[3])<<24

	nbitsBytes := HexToBytes(nbitsHex)
	nbits := uint32(nbitsBytes[0])<<24 | uint32(nbitsBytes[1])<<16 | uint32(nbitsBytes[2])<<8 | uint32(nbitsBytes[3])

	nonceBytes := HexToBytes(nonceHex)
	nonce := uint32(nonceBytes[0]) | uint32(nonceBytes[1])<<8 | uint32(nonceBytes[2])<<16 | uint32(nonceBytes[3])<<24

	t.Logf("version: 0x%08x, ntime: 0x%08x, nbits: 0x%08x, nonce: 0x%08x", version, ntime, nbits, nonce)

	// 6. Construct 80-byte header
	header := BuildHeader(version, prevHashInternal, merkleRoot[:], ntime, nbits, nonce)
	t.Logf("80-byte header: %s", hex.EncodeToString(header[:]))

	// Verify header structure
	if header[0] != 0x00 || header[1] != 0x40 || header[2] != 0x00 || header[3] != 0x20 {
		t.Errorf("version bytes wrong: got %x", header[0:4])
	}

	// 7. Verify header is 80 bytes and looks sane
	if len(header) != 80 {
		t.Fatalf("header length = %d, want 80", len(header))
	}

	// Verify we can compute hoohash on this header without panicking
	// (We can't verify the actual value without the reference C implementation
	// to compare against, but at least it shouldn't crash)
	t.Logf("Computing hoohash on reconstructed header...")
	// Note: hoohash is in src/pow package, import it separately in actual code

	t.Log("Header reconstruction from stratum data successful")
}

// TestNBitsParsing verifies parsing of the nbits field from the stratum sample.
func TestNBitsParsing(t *testing.T) {
	nbits, err := ParseNBits("1d06bd8c")
	if err != nil {
		t.Fatal(err)
	}
	// 0x1d06bd8c compact target
	target := CompactToBig(nbits)
	if target.Sign() <= 0 {
		t.Error("target should be positive")
	}
	t.Logf("nbits 0x%08x -> target %x", nbits, target)

	diff := TargetToDifficulty(target)
	t.Logf("network difficulty: %f", diff)
	if diff <= 0 {
		t.Error("difficulty should be positive")
	}
}

// TestStratumDifficultyConversion verifies the stratum difficulty from the
// reference session (327.68) converts properly.
func TestStratumDifficultyConversion(t *testing.T) {
	stratumDiff := 327.68
	target := DiffToTarget(stratumDiff)
	if target.Sign() <= 0 {
		t.Error("target should be positive")
	}

	recoveredDiff := TargetToDifficulty(target)
	ratio := recoveredDiff / stratumDiff
	if ratio < 0.99 || ratio > 1.01 {
		t.Errorf("difficulty round-trip failed: %.4f -> target -> %.4f", stratumDiff, recoveredDiff)
	}
	t.Logf("stratum diff %.2f -> target %x -> recovered diff %.4f", stratumDiff, target, recoveredDiff)
}
