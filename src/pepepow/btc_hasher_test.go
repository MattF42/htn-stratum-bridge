package pepepow

import (
	"encoding/hex"
	"math/big"
	"testing"
)

func TestDoubleSHA256(t *testing.T) {
	// Known Bitcoin test vector: SHA256d("") =
	// 5df6e0e2761359d30a8275058e299fcc0381534545f55cf43e41983f5d4c9456
	data := []byte("")
	result := DoubleSHA256(data)
	expected := "5df6e0e2761359d30a8275058e299fcc0381534545f55cf43e41983f5d4c9456"
	got := hex.EncodeToString(result[:])
	if got != expected {
		t.Errorf("DoubleSHA256(\"\") = %s, want %s", got, expected)
	}
}

func TestBuildHeader(t *testing.T) {
	header := BuildHeader(0x20004000, make([]byte, 32), make([]byte, 32), 0x69ddf717, 0x1d06bd8c, 0x03ab46e2)
	if len(header) != 80 {
		t.Fatalf("header length = %d, want 80", len(header))
	}
	// Check version bytes (LE)
	if header[0] != 0x00 || header[1] != 0x40 || header[2] != 0x00 || header[3] != 0x20 {
		t.Errorf("version bytes = %x, want 00400020", header[0:4])
	}
	// Check nonce bytes (LE)
	if header[76] != 0xe2 || header[77] != 0x46 || header[78] != 0xab || header[79] != 0x03 {
		t.Errorf("nonce bytes = %x, want e246ab03", header[76:80])
	}
}

func TestBuildMerkleRootEmpty(t *testing.T) {
	// With no branches, merkle root = coinbase hash
	var coinbaseHash [32]byte
	coinbaseHash[0] = 0x42
	root := BuildMerkleRoot(coinbaseHash, nil)
	if root != coinbaseHash {
		t.Error("merkle root with no branches should equal coinbase hash")
	}
}

func TestBuildMerkleRootOneBranch(t *testing.T) {
	var coinbaseHash [32]byte
	coinbaseHash[0] = 0x01
	branch := make([]byte, 32)
	branch[0] = 0x02

	root := BuildMerkleRoot(coinbaseHash, [][]byte{branch})
	// Result should be double-SHA256 of coinbaseHash + branch
	var combined [64]byte
	copy(combined[0:32], coinbaseHash[:])
	copy(combined[32:64], branch)
	expected := DoubleSHA256(combined[:])

	if root != expected {
		t.Errorf("merkle root = %x, want %x", root, expected)
	}
}

func TestCompactToBig(t *testing.T) {
	// Bitcoin difficulty 1 target: 0x1d00ffff
	target := CompactToBig(0x1d00ffff)
	expected, _ := new(big.Int).SetString("00000000FFFF0000000000000000000000000000000000000000000000000000", 16)
	if target.Cmp(expected) != 0 {
		t.Errorf("CompactToBig(0x1d00ffff) = %x, want %x", target, expected)
	}
}

func TestDiffToTargetRoundTrip(t *testing.T) {
	diff := 100.0
	target := DiffToTarget(diff)
	recoveredDiff := TargetToDifficulty(target)
	// Should be approximately equal (floating point)
	ratio := recoveredDiff / diff
	if ratio < 0.99 || ratio > 1.01 {
		t.Errorf("DiffToTarget/TargetToDifficulty round trip: got %f, want ~%f", recoveredDiff, diff)
	}
}

func TestReverseBytes(t *testing.T) {
	input := []byte{0x01, 0x02, 0x03, 0x04}
	expected := []byte{0x04, 0x03, 0x02, 0x01}
	result := ReverseBytes(input)
	for i, b := range result {
		if b != expected[i] {
			t.Errorf("ReverseBytes[%d] = %x, want %x", i, b, expected[i])
		}
	}
}

func TestSwapEndian32(t *testing.T) {
	input := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}
	expected := []byte{0x04, 0x03, 0x02, 0x01, 0x08, 0x07, 0x06, 0x05}
	result := SwapEndian32(input)
	for i, b := range result {
		if b != expected[i] {
			t.Errorf("SwapEndian32[%d] = %x, want %x", i, b, expected[i])
		}
	}
}

func TestDecodeBase58Check(t *testing.T) {
	// Test with a known Bitcoin address (1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa - Satoshi's address)
	// This is base58check encoded with version byte 0x00
	decoded := decodeBase58Check("1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa")
	if decoded == nil {
		t.Fatal("failed to decode valid base58check address")
	}
	if decoded[0] != 0x00 {
		t.Errorf("version byte = %x, want 0x00", decoded[0])
	}
	if len(decoded) != 21 {
		t.Errorf("decoded length = %d, want 21", len(decoded))
	}
}

func TestBuildCoinbaseTx(t *testing.T) {
	payoutScript := P2PKHScript(make([]byte, 20))
	cb1, cb2 := BuildCoinbaseTx(100, 5000000000, payoutScript, 8, 1700000000, "/test/")

	// Both should be valid hex
	_, err1 := hex.DecodeString(cb1)
	_, err2 := hex.DecodeString(cb2)
	if err1 != nil {
		t.Errorf("coinbase1 is not valid hex: %v", err1)
	}
	if err2 != nil {
		t.Errorf("coinbase2 is not valid hex: %v", err2)
	}
	if cb1 == "" || cb2 == "" {
		t.Error("coinbase parts should not be empty")
	}
}

func TestEncodeScriptNum(t *testing.T) {
	tests := []struct {
		n    int64
		want []byte
	}{
		{0, []byte{0}},
		{1, []byte{1}},
		{127, []byte{0x7f}},
		{128, []byte{0x80, 0x00}},
		{255, []byte{0xff, 0x00}},
		{256, []byte{0x00, 0x01}},
		{100000, []byte{0xa0, 0x86, 0x01}},
	}
	for _, tt := range tests {
		got := encodeScriptNum(tt.n)
		if len(got) != len(tt.want) {
			t.Errorf("encodeScriptNum(%d) length = %d, want %d", tt.n, len(got), len(tt.want))
			continue
		}
		for i, b := range got {
			if b != tt.want[i] {
				t.Errorf("encodeScriptNum(%d)[%d] = %x, want %x", tt.n, i, b, tt.want[i])
			}
		}
	}
}
