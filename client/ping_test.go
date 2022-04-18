package client

import (
	"testing"
)

func TestJitter(t *testing.T) {
	v, j := 1.0, 0.2
	r := float64(Jitter(v, j))

	if 0.8 > r || r > 1.2 {
		t.Errorf("expected %f with jitter of %f to be between 0.8 and 0.2, got %f", v, j, r)
	}
}
