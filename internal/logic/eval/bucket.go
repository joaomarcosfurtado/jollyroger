package eval

import (
	"crypto/sha256"
	"encoding/binary"
)

// BucketCount is the number of buckets; one bucket is 0.001% of traffic.
const BucketCount = 100000

// Bucket deterministically maps (flagKey, salt, value) to [0, BucketCount). The same inputs give
// the same bucket in every SDK: the first 4 bytes of SHA-256(flagKey + "." + salt + "." + value),
// read as a big-endian uint32, modulo BucketCount. Strings are hashed as UTF-8.
func Bucket(flagKey, salt, value string) uint32 {
	sum := sha256.Sum256([]byte(flagKey + "." + salt + "." + value))
	return binary.BigEndian.Uint32(sum[:4]) % BucketCount
}
