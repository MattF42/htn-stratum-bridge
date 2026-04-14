package pow

import (
	"encoding/binary"
	"math"
	"math/bits"

	"lukechampine.com/blake3"
)

// HoohashV110Bitcoin computes the HoohashV110 proof-of-work for an 80-byte
// Bitcoin-style block header, as used by PePePow.  The algorithm:
//
//  1. BLAKE3(full 80-byte header) → firstPass
//  2. BLAKE3(header with nonce bytes [76:80] zeroed) → matrixSeed
//  3. Generate 64×64 float matrix from matrixSeed via xoshiro256**
//  4. Read nonce from header[76:80] as little-endian uint32
//  5. Matrix multiplication with complex non-linear transforms
//  6. Final BLAKE3 hash
//  7. Reverse to little-endian byte order
//
// Returns the 32-byte PoW hash in little-endian order (ready for comparison
// with the target).  Returns all-0xFF on invalid input.
func HoohashV110Bitcoin(header []byte) [32]byte {
	var out [32]byte
	if len(header) != 80 {
		for i := range out {
			out[i] = 0xFF
		}
		return out
	}

	// 1) Nonce-dependent first pass: BLAKE3(full header)
	h1 := blake3.Sum256(header)

	// 2) Nonce-independent matrix seed: BLAKE3(header with nonce zeroed)
	var masked [80]byte
	copy(masked[:], header)
	masked[76] = 0
	masked[77] = 0
	masked[78] = 0
	masked[79] = 0
	matrixSeed := blake3.Sum256(masked[:])

	// 3) Generate float matrix from matrixSeed
	mat := generatePepepowMatrix(matrixSeed[:])

	// 4) Read nonce from header[76:80] (little-endian uint32)
	nonce := uint64(binary.LittleEndian.Uint32(header[76:80]))

	// 5) Matrix multiplication PoW
	result := pepepowMatrixMultiplication(mat, h1[:], nonce)

	// 6) Final BLAKE3 hash
	finalHash := blake3.Sum256(result[:])

	// 7) Reverse to little-endian order
	for i := 0; i < 32; i++ {
		out[i] = finalHash[31-i]
	}
	return out
}

// generatePepepowMatrix creates the 64×64 float64 matrix used by HoohashV110
// for PePePow.  Identical to the C reference: xoshiro256** seeded from the
// 32-byte matrixSeed, each cell = lower32(gen()) / UINT32_MAX * 1000000.
func generatePepepowMatrix(seed []byte) *[64][64]float64 {
	var mat [64][64]float64
	gen := newPepepowXoshiro(seed)
	const normalize = 1000000.0
	for i := 0; i < 64; i++ {
		for j := 0; j < 64; j++ {
			val := gen.next()
			lower4 := uint32(val & 0xFFFFFFFF)
			mat[i][j] = float64(lower4) / float64(math.MaxUint32) * normalize
		}
	}
	return &mat
}

// pepepowXoshiro implements the xoshiro256** PRNG used by PePePow.
type pepepowXoshiro struct {
	s0, s1, s2, s3 uint64
}

func newPepepowXoshiro(seed []byte) *pepepowXoshiro {
	x := &pepepowXoshiro{}
	x.s0 = binary.LittleEndian.Uint64(seed[0:8])
	x.s1 = binary.LittleEndian.Uint64(seed[8:16])
	x.s2 = binary.LittleEndian.Uint64(seed[16:24])
	x.s3 = binary.LittleEndian.Uint64(seed[24:32])
	return x
}

func (x *pepepowXoshiro) next() uint64 {
	res := bits.RotateLeft64(x.s0+x.s3, 23) + x.s0
	t := x.s1 << 17

	x.s2 ^= x.s0
	x.s3 ^= x.s1
	x.s1 ^= x.s2
	x.s0 ^= x.s3

	x.s2 ^= t
	x.s3 = bits.RotateLeft64(x.s3, 45)

	return res
}

// convertBytesToUint32ArrayBE reads 8 big-endian uint32 values from 32 bytes.
func convertBytesToUint32ArrayBE(b []byte) [8]uint32 {
	var H [8]uint32
	for i := 0; i < 8; i++ {
		H[i] = binary.BigEndian.Uint32(b[i*4 : i*4+4])
	}
	return H
}

// pepepowComplexNonLinear110 matches the C reference ComplexNonLinear function.
func pepepowComplexNonLinear110(x float64) float64 {
	const complexTransformMultiplier = 0.000001
	tf1 := (x*complexTransformMultiplier)/8.0 - math.Floor((x*complexTransformMultiplier)/8.0)
	tf2 := (x*complexTransformMultiplier)/4.0 - math.Floor((x*complexTransformMultiplier)/4.0)

	if tf1 < 0.33 {
		if tf2 < 0.25 {
			return pepepowMediumComplex(x + (1 + tf2))
		} else if tf2 < 0.5 {
			return pepepowMediumComplex(x - (1 + tf2))
		} else if tf2 < 0.75 {
			return pepepowMediumComplex(x * (1 + tf2))
		}
		return pepepowMediumComplex(x / (1 + tf2))
	} else if tf1 < 0.66 {
		if tf2 < 0.25 {
			return pepepowIntermediateComplex(x + (1 + tf2))
		} else if tf2 < 0.5 {
			return pepepowIntermediateComplex(x - (1 + tf2))
		} else if tf2 < 0.75 {
			return pepepowIntermediateComplex(x * (1 + tf2))
		}
		return pepepowIntermediateComplex(x / (1 + tf2))
	} else {
		if tf2 < 0.25 {
			return pepepowHighComplex(x + (1 + tf2))
		} else if tf2 < 0.5 {
			return pepepowHighComplex(x - (1 + tf2))
		} else if tf2 < 0.75 {
			return pepepowHighComplex(x * (1 + tf2))
		}
		return pepepowHighComplex(x / (1 + tf2))
	}
}

func pepepowMediumComplex(x float64) float64 {
	return math.Exp(math.Sin(x) + math.Cos(x))
}

func pepepowIntermediateComplex(x float64) float64 {
	const eps = 1e-9
	if math.Abs(x-math.Pi/2) < eps || math.Abs(x-3*math.Pi/2) < eps {
		return 0
	}
	return math.Sin(x) * math.Sin(x)
}

func pepepowHighComplex(x float64) float64 {
	return 1.0 / math.Sqrt(math.Abs(x)+1)
}

// pepepowSafeComplexTransform matches the C SafeComplexTransform function.
func pepepowSafeComplexTransform(input float64) float64 {
	rounds := 1.0
	tv := pepepowComplexNonLinear110(input)
	for math.IsNaN(tv) || math.IsInf(tv, 0) {
		input *= 0.1
		if input <= 0.0000000000001 {
			return 0 * rounds
		}
		rounds++
		tv = pepepowComplexNonLinear110(input)
	}
	return tv * rounds
}

func pepepowTransformFactor(x float64) float64 {
	const granularity = 1024.0
	return x/granularity - math.Floor(x/granularity)
}

// pepepowMatrixMultiplication performs the HoohashV110 matrix multiplication.
// hashBytes is the 32-byte BLAKE3 of the full 80-byte header.
func pepepowMatrixMultiplication(mat *[64][64]float64, hashBytes []byte, nonce uint64) [32]byte {
	var scaledValues [32]byte
	var vector [64]byte
	var product [64]float64
	var result [32]byte

	H := convertBytesToUint32ArrayBE(hashBytes)
	hashXor := float64(H[0] ^ H[1] ^ H[2] ^ H[3] ^ H[4] ^ H[5] ^ H[6] ^ H[7])
	nonceMod := float64(nonce & 0xFF)
	divider := 0.0001
	multiplier := 1234.0
	sw := 0.0

	for i := 0; i < 32; i++ {
		vector[2*i] = hashBytes[i] >> 4
		vector[2*i+1] = hashBytes[i] & 0x0F
	}

	for i := 0; i < 64; i++ {
		for j := 0; j < 64; j++ {
			if sw <= 0.02 {
				input := mat[i][j]*hashXor*float64(vector[j]) + nonceMod
				outputVal := pepepowSafeComplexTransform(input) * float64(vector[j]) * multiplier
				product[i] += outputVal
			} else {
				outputVal := mat[i][j] * divider * float64(vector[j])
				product[i] += outputVal
			}
			sw = pepepowTransformFactor(product[i])
		}
	}

	for i := 0; i < 64; i += 2 {
		pval := uint64(product[i]) + uint64(product[i+1])
		scaledValues[i/2] = byte(pval & 0xFF)
	}

	for i := 0; i < 32; i++ {
		result[i] = hashBytes[i] ^ scaledValues[i]
	}

	return result
}
