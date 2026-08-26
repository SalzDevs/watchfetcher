package sources

import (
	"crypto/rand"
	"crypto/sha256"
	"time"
)

func randBytes(n int) []byte {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return b
}

func sha256Sum(s string) []byte {
	sum := sha256.Sum256([]byte(s))
	return sum[:]
}

func nowFloat() float64 {
	return float64(time.Now().UnixNano()) / 1e9
}
