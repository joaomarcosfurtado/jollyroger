package eval

import (
	"strconv"
	"testing"
)

func TestBucket_MatchesIndependentlyComputedValues(t *testing.T) {
	t.Parallel()
	// Expected values computed with Python hashlib and cross-checked with sha256sum, not with
	// the code under test.
	cases := []struct {
		flagKey, salt, value string
		want                 uint32
	}{
		{"new-checkout", "", "user-1", 83561},
		{"new-checkout", "s1", "user-1", 16082},
		{"new-checkout", "", "user-2", 41251},
		{"a", "b", "c", 65252},
		{"flag.with-dots_1", "salt", "\u00fcmlaut-user", 77465},
		{"x", "", "", 30962},
	}
	for _, tc := range cases {
		if got := Bucket(tc.flagKey, tc.salt, tc.value); got != tc.want {
			t.Errorf("Bucket(%q, %q, %q) = %d, want %d", tc.flagKey, tc.salt, tc.value, got, tc.want)
		}
	}
}

func TestBucket_StaysInRange(t *testing.T) {
	t.Parallel()
	for i := range 10000 {
		if b := Bucket("range-check", "", "user-"+strconv.Itoa(i)); b >= BucketCount {
			t.Fatalf("bucket %d out of range", b)
		}
	}
}
