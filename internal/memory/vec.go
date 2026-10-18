package memory

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"math"
)

// MaxVectorDim bounds a stored embedding; real models are a few thousand at most.
const MaxVectorDim = 8192

// Hash is the content key used by the embedding cache and the window hash in records.
func Hash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// Cosine returns 0 for missing, empty, mismatched or zero vectors rather than failing,
// so one bad vector can never break a turn.
func Cosine(a, b []float32) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		x, y := float64(a[i]), float64(b[i])
		dot += x * y
		na += x * x
		nb += y * y
	}
	if na == 0 || nb == 0 {
		return 0
	}
	v := dot / (math.Sqrt(na) * math.Sqrt(nb))
	if !finite(v) {
		return 0
	}
	return v
}

// EncodeVector stores a vector as little-endian float32.
func EncodeVector(v []float32) []byte {
	out := make([]byte, 4*len(v))
	for i, f := range v {
		binary.LittleEndian.PutUint32(out[4*i:], math.Float32bits(f))
	}
	return out
}

// DecodeVector reverses EncodeVector and rejects wrong sizes and non-finite values.
func DecodeVector(b []byte, dim int) ([]float32, error) {
	if dim <= 0 || dim > MaxVectorDim || len(b) != 4*dim {
		return nil, errors.New("embedding vector has an invalid size")
	}
	out := make([]float32, dim)
	for i := range out {
		f := math.Float32frombits(binary.LittleEndian.Uint32(b[4*i:]))
		if math.IsNaN(float64(f)) || math.IsInf(float64(f), 0) {
			return nil, errors.New("embedding vector contains a non-finite value")
		}
		out[i] = f
	}
	return out, nil
}

// ValidVector reports whether v is a usable embedding.
func ValidVector(v []float32) bool {
	if len(v) == 0 || len(v) > MaxVectorDim {
		return false
	}
	for _, f := range v {
		if math.IsNaN(float64(f)) || math.IsInf(float64(f), 0) {
			return false
		}
	}
	return true
}
