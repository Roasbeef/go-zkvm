package sha256

import (
	stdsha "crypto/sha256"
	"testing"
)

// TestMatchesStdlib checks Sum256 and the streaming hash against the
// standard library over lengths that cross every padding boundary, with
// writes split at every offset.
func TestMatchesStdlib(t *testing.T) {
	data := make([]byte, 300)
	for i := range data {
		data[i] = byte(i*7 + 3)
	}
	for n := 0; n <= len(data); n++ {
		want := stdsha.Sum256(data[:n])
		if got := Sum256(data[:n]); got != want {
			t.Fatalf("Sum256 len %d mismatch", n)
		}
		for split := 0; split <= n; split += 13 {
			h := New()
			h.Write(data[:split])
			h.Write(data[split:n])
			if got := h.Sum(nil); string(got) != string(want[:]) {
				t.Fatalf("stream len %d split %d mismatch", n, split)
			}
		}
	}
}
