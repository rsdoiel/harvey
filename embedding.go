package harvey

import (
	"bytes"
	"encoding/binary"
	"math"
)

/** Embedder converts text to a dense vector. Memory recall uses it; nothing
 * supplies one since /rag was removed (workspace DR-0035), so memory is by
 * keyword until the memory store is replaced (DR-0037).
 *
 * Methods:
 *   Embed(text string) ([]float64, error) — return a vector for text.
 *   Name() string                         — return the embedding model name.
 *
 * Example:
 *   vec, err := embedder.Embed("hello world")
 */
type Embedder interface {
	Embed(text string) ([]float64, error)
	Name() string
}

// cosineSimilarity returns the cosine similarity of two equal-length vectors.
// Returns 0 when either vector has zero magnitude or the lengths differ.
func cosineSimilarity(a, b []float64) float64 {
	if len(a) != len(b) {
		return 0
	}
	var dot, magA, magB float64
	for i := range a {
		dot += a[i] * b[i]
		magA += a[i] * a[i]
		magB += b[i] * b[i]
	}
	if magA == 0 || magB == 0 {
		return 0
	}
	return dot / (math.Sqrt(magA) * math.Sqrt(magB))
}

// serialize encodes a float64 slice as [int32 length][float64...] in
// little-endian byte order.
func serialize(vec []float64) ([]byte, error) {
	buf := new(bytes.Buffer)
	if err := binary.Write(buf, binary.LittleEndian, int32(len(vec))); err != nil {
		return nil, err
	}
	for _, v := range vec {
		if err := binary.Write(buf, binary.LittleEndian, v); err != nil {
			return nil, err
		}
	}
	return buf.Bytes(), nil
}

// deserialize decodes a byte slice produced by serialize into a float64 slice.
func deserialize(data []byte) ([]float64, error) {
	buf := bytes.NewReader(data)
	var length int32
	if err := binary.Read(buf, binary.LittleEndian, &length); err != nil {
		return nil, err
	}
	vec := make([]float64, length)
	for i := range vec {
		if err := binary.Read(buf, binary.LittleEndian, &vec[i]); err != nil {
			return nil, err
		}
	}
	return vec, nil
}
