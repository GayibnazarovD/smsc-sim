package smsc

import (
	"math/rand"
	"time"

	"github.com/dilshodgayibnazarov/smsc-sim/internal/config"
)

// sampleLatency draws a submit_sm_resp delay from the configured distribution.
func sampleLatency(c config.Latency, rng *rand.Rand) time.Duration {
	mean := c.Mean.D()
	min, max := c.Min.D(), c.Max.D()

	switch c.Dist {
	case "uniform":
		lo, hi := min, max
		if hi <= 0 {
			hi = 2 * mean
		}
		if hi <= lo {
			return lo
		}
		return lo + time.Duration(rng.Int63n(int64(hi-lo)))
	case "exponential":
		d := time.Duration(rng.ExpFloat64() * float64(mean))
		if max > 0 && d > max {
			d = max
		}
		if d < min {
			d = min
		}
		return d
	default: // "fixed"
		d := mean
		if j := c.Jitter.D(); j > 0 {
			d += time.Duration(rng.Int63n(int64(2*j+1))) - j
		}
		if d < 0 {
			d = 0
		}
		return d
	}
}

// pct reports true with probability p (0-100).
func pct(rng *rand.Rand, p float64) bool {
	return rng.Float64()*100 < p
}
