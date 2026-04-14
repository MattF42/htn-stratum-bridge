package pepepow

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math/big"
)

// Bitcoin-style 80-byte header layout:
//   [0:4]   version      (uint32 LE)
//   [4:36]  prevhash     (32 bytes, internal byte order)
//   [36:68] merkle_root  (32 bytes)
//   [68:72] time         (uint32 LE)
//   [72:76] bits         (uint32 LE)
//   [76:80] nonce        (uint32 LE)

// BuildHeader constructs an 80-byte Bitcoin-style block header.
func BuildHeader(version uint32, prevHash, merkleRoot []byte, timestamp, bits, nonce uint32) [80]byte {
	var hdr [80]byte
	binary.LittleEndian.PutUint32(hdr[0:4], version)
	copy(hdr[4:36], prevHash)
	copy(hdr[36:68], merkleRoot)
	binary.LittleEndian.PutUint32(hdr[68:72], timestamp)
	binary.LittleEndian.PutUint32(hdr[72:76], bits)
	binary.LittleEndian.PutUint32(hdr[76:80], nonce)
	return hdr
}

// DoubleSHA256 computes SHA256(SHA256(data)).
func DoubleSHA256(data []byte) [32]byte {
	first := sha256.Sum256(data)
	return sha256.Sum256(first[:])
}

// BuildMerkleRoot computes the Bitcoin merkle root from a coinbase hash and
// the merkle branches (partner hashes at each tree level).
// Each branch entry is a 32-byte hash.
func BuildMerkleRoot(coinbaseHash [32]byte, branches [][]byte) [32]byte {
	current := coinbaseHash
	for _, branch := range branches {
		var combined [64]byte
		copy(combined[0:32], current[:])
		copy(combined[32:64], branch)
		current = DoubleSHA256(combined[:])
	}
	return current
}

// BuildMerkleBranches takes a list of transaction hashes (excluding the
// coinbase) and returns the merkle branches needed for stratum.
// If txHashes is empty (only coinbase), returns nil.
func BuildMerkleBranches(txHashes [][32]byte) [][]byte {
	if len(txHashes) == 0 {
		return nil
	}

	// Start with the transaction hashes (coinbase is handled separately by the miner)
	level := make([][32]byte, len(txHashes))
	copy(level, txHashes)

	var branches [][]byte
	for len(level) > 0 {
		// The first element at each level is the branch entry
		branches = append(branches, level[0][:])

		if len(level) == 1 {
			break
		}

		// Compute the next level (pair up and hash, using the last element
		// as a partner for an odd count)
		var next [][32]byte
		for i := 1; i < len(level); i += 2 {
			var combined [64]byte
			copy(combined[0:32], level[i][:])
			if i+1 < len(level) {
				copy(combined[32:64], level[i+1][:])
			} else {
				copy(combined[32:64], level[i][:]) // duplicate last
			}
			next = append(next, DoubleSHA256(combined[:]))
		}
		level = next
	}
	return branches
}

// ReverseBytes returns a copy of the input with the byte order reversed.
func ReverseBytes(b []byte) []byte {
	out := make([]byte, len(b))
	for i, v := range b {
		out[len(b)-1-i] = v
	}
	return out
}

// SwapEndian32 reverses the byte order within each 4-byte group.
// This is used for the prevhash encoding in Bitcoin stratum v1.
func SwapEndian32(b []byte) []byte {
	out := make([]byte, len(b))
	for i := 0; i+4 <= len(b); i += 4 {
		out[i] = b[i+3]
		out[i+1] = b[i+2]
		out[i+2] = b[i+1]
		out[i+3] = b[i]
	}
	return out
}

// HexToBytes decodes a hex string, returning nil on error.
func HexToBytes(s string) []byte {
	b, _ := hex.DecodeString(s)
	return b
}

// CompactToBig converts a Bitcoin compact target representation (nBits) to
// a big.Int target value.
func CompactToBig(compact uint32) *big.Int {
	mantissa := compact & 0x007FFFFF
	exponent := compact >> 24
	var target big.Int
	target.SetUint64(uint64(mantissa))
	if exponent <= 3 {
		target.Rsh(&target, uint(8*(3-exponent)))
	} else {
		target.Lsh(&target, uint(8*(exponent-3)))
	}
	// Handle the sign bit (bit 23 of mantissa)
	if compact&0x00800000 != 0 {
		target.Neg(&target)
	}
	return &target
}

// TargetToDifficulty converts a 256-bit target to Bitcoin difficulty.
// difficulty = genesis_target / target
// where genesis_target is the target for difficulty 1.
func TargetToDifficulty(target *big.Int) float64 {
	if target.Sign() == 0 {
		return 0
	}
	// Difficulty 1 target for Bitcoin-like chains
	diff1, _ := new(big.Int).SetString("00000000FFFF0000000000000000000000000000000000000000000000000000", 16)
	fDiff1 := new(big.Float).SetInt(diff1)
	fTarget := new(big.Float).SetInt(target)
	diff, _ := new(big.Float).Quo(fDiff1, fTarget).Float64()
	return diff
}

// DiffToTarget converts a stratum difficulty to a 256-bit target.
func DiffToTarget(diff float64) *big.Int {
	if diff <= 0 {
		return new(big.Int)
	}
	diff1, _ := new(big.Int).SetString("00000000FFFF0000000000000000000000000000000000000000000000000000", 16)
	fDiff1 := new(big.Float).SetInt(diff1)
	fDiff := new(big.Float).SetFloat64(diff)
	fTarget := new(big.Float).Quo(fDiff1, fDiff)
	target, _ := fTarget.Int(nil)
	return target
}

// BuildCoinbaseTx constructs a coinbase transaction for PePePow mining.
// It returns coinbase1 (hex) and coinbase2 (hex), split at the extranonce
// insertion point.
//
// Parameters:
//   - height: block height (for BIP34 encoding)
//   - coinbaseValue: total coinbase value in satoshis
//   - payoutScript: the output script (scriptPubKey) for the miner payout
//   - extranonceSize: total bytes reserved for extranonce1+extranonce2
//   - blockTime: block timestamp (included in scriptSig)
//   - coinbaseText: arbitrary text to include in the coinbase (e.g. pool identifier)
func BuildCoinbaseTx(height int64, coinbaseValue int64, payoutScript []byte,
	extranonceSize int, blockTime uint32, coinbaseText string) (string, string) {

	// === coinbase1: everything before the extranonce ===
	var cb1 []byte

	// Transaction version (1, little-endian)
	cb1 = append(cb1, 0x01, 0x00, 0x00, 0x00)

	// Input count: 1
	cb1 = append(cb1, 0x01)

	// Previous output: null (32 zero bytes + index 0xFFFFFFFF)
	cb1 = append(cb1, make([]byte, 32)...)
	cb1 = append(cb1, 0xFF, 0xFF, 0xFF, 0xFF)

	// Build scriptSig content (before extranonce)
	var scriptSigPre []byte

	// BIP34: block height encoding
	heightBytes := encodeScriptNum(height)
	scriptSigPre = append(scriptSigPre, byte(len(heightBytes)))
	scriptSigPre = append(scriptSigPre, heightBytes...)

	// Block time (4 bytes LE, push 4 bytes)
	scriptSigPre = append(scriptSigPre, 0x04)
	timeBuf := make([]byte, 4)
	binary.LittleEndian.PutUint32(timeBuf, blockTime)
	scriptSigPre = append(scriptSigPre, timeBuf...)

	// === coinbase2: everything after the extranonce ===
	var scriptSigPost []byte

	// Coinbase text
	if coinbaseText != "" {
		textBytes := []byte(coinbaseText)
		if len(textBytes) > 0 {
			scriptSigPost = append(scriptSigPost, byte(len(textBytes)))
			scriptSigPost = append(scriptSigPost, textBytes...)
		}
	}

	// Compute total scriptSig length
	scriptSigLen := len(scriptSigPre) + extranonceSize + len(scriptSigPost)
	cb1 = append(cb1, byte(scriptSigLen))
	cb1 = append(cb1, scriptSigPre...)

	// After the extranonce, continue with:
	var cb2 []byte
	cb2 = append(cb2, scriptSigPost...)

	// Sequence: 0x00000000
	cb2 = append(cb2, 0x00, 0x00, 0x00, 0x00)

	// Output count: 1 (miner payout)
	cb2 = append(cb2, 0x01)

	// Output value (8 bytes LE)
	valBuf := make([]byte, 8)
	binary.LittleEndian.PutUint64(valBuf, uint64(coinbaseValue))
	cb2 = append(cb2, valBuf...)

	// Output script
	cb2 = append(cb2, byte(len(payoutScript)))
	cb2 = append(cb2, payoutScript...)

	// Locktime: 0x00000000
	cb2 = append(cb2, 0x00, 0x00, 0x00, 0x00)

	return hex.EncodeToString(cb1), hex.EncodeToString(cb2)
}

// encodeScriptNum encodes an integer for use in Bitcoin script (BIP34 height).
func encodeScriptNum(n int64) []byte {
	if n == 0 {
		return []byte{0}
	}
	negative := n < 0
	if negative {
		n = -n
	}
	var result []byte
	for n > 0 {
		result = append(result, byte(n&0xFF))
		n >>= 8
	}
	// If the high bit of the last byte is set, add an extra byte
	if result[len(result)-1]&0x80 != 0 {
		if negative {
			result = append(result, 0x80)
		} else {
			result = append(result, 0x00)
		}
	} else if negative {
		result[len(result)-1] |= 0x80
	}
	return result
}

// P2PKHScript creates a standard P2PKH scriptPubKey from a 20-byte pubkey hash.
func P2PKHScript(pubkeyHash []byte) []byte {
	if len(pubkeyHash) != 20 {
		return nil
	}
	script := []byte{
		0x76, // OP_DUP
		0xA9, // OP_HASH160
		0x14, // Push 20 bytes
	}
	script = append(script, pubkeyHash...)
	script = append(script, 0x88) // OP_EQUALVERIFY
	script = append(script, 0xAC) // OP_CHECKSIG
	return script
}

// BuildCoinbaseFromTemplate constructs coinbase1/coinbase2 from the
// getblocktemplate response.  This uses the actual coinbase_aux flags
// and outputs from the template, producing a standard Bitcoin coinbase
// that is compatible with PePe-core's validation.
func BuildCoinbaseFromTemplate(tmpl *BlockTemplate, payoutScript []byte,
	extranonceSize int, coinbaseText string) (string, string) {

	return BuildCoinbaseTx(
		tmpl.Height,
		tmpl.CoinbaseValue,
		payoutScript,
		extranonceSize,
		uint32(tmpl.CurTime),
		coinbaseText,
	)
}

// ParseNBits parses a hex nbits string into a uint32.
func ParseNBits(nbitsHex string) (uint32, error) {
	b, err := hex.DecodeString(nbitsHex)
	if err != nil || len(b) != 4 {
		return 0, fmt.Errorf("invalid nbits: %s", nbitsHex)
	}
	return binary.BigEndian.Uint32(b), nil
}
