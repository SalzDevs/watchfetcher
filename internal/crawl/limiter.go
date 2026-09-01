package crawl

import (
	"context"
	"math/rand"
	"sync"
	"time"
)

// per-source polite intervals (avg time between HTTP requests).
// Chrono24 is most sensitive — 3.2s avg; 1916 is API, can be faster.
func intervalFor(source string) time.Duration {
	switch source {
	case "chrono24":
		return 3200 * time.Millisecond
	case "watchfinder":
		return 2500 * time.Millisecond
	case "bobswatches":
		return 1800 * time.Millisecond
	case "the1916company":
		return 900 * time.Millisecond
	default:
		return 2000 * time.Millisecond
	}
}

type limiter struct {
	interval time.Duration
	mu       sync.Mutex
	last     time.Time
}

var (
	globalMu sync.Mutex
	limiters = map[string]*limiter{}
)

func getLimiter(source string) *limiter {
	globalMu.Lock()
	defer globalMu.Unlock()
	if l, ok := limiters[source]; ok {
		return l
	}
	l := &limiter{interval: intervalFor(source)}
	limiters[source] = l
	return l
}

// Wait blocks until the source's polite interval has elapsed since the last
// request, with ±30% jitter to avoid synchronized bursts. Safe for concurrent
// use; calls for the same source are serialized.
func Wait(ctx context.Context, source string) error {
	l := getLimiter(source)
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(l.last)
	wait := l.interval - elapsed
	// jitter ±30% of interval
	jitter := time.Duration(float64(l.interval) * (rand.Float64()*0.6 - 0.3))
	wait += jitter
	if wait > 0 {
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	l.last = time.Now()
	return nil
}

// Jitter returns a duration around d with ±30% randomization.
func Jitter(d time.Duration) time.Duration {
	if d <= 0 {
		return 0
	}
	j := float64(d) * (rand.Float64()*0.6 - 0.3)
	return d + time.Duration(j)
}
