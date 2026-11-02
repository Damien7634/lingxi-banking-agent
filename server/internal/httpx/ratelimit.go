package httpx

import (
	"sync"
	"time"
)

// Limiter is an in-memory token bucket keyed by an arbitrary string (IP, user,
// user+action). Good for a single instance; swap for Redis when scaling out.
type Limiter struct {
	mu      sync.Mutex
	rate    float64 // tokens per second
	burst   float64
	buckets map[string]*bucket
}

type bucket struct {
	tokens float64
	last   time.Time
}

func NewLimiter(perMinute int, burst int) *Limiter {
	l := &Limiter{rate: float64(perMinute) / 60, burst: float64(burst), buckets: map[string]*bucket{}}
	go l.gc()
	return l
}

func (l *Limiter) Allow(key string) bool {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{tokens: l.burst, last: now}
		l.buckets[key] = b
	}
	b.tokens = min(l.burst, b.tokens+now.Sub(b.last).Seconds()*l.rate)
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

func (l *Limiter) gc() {
	t := time.NewTicker(5 * time.Minute)
	for range t.C {
		cutoff := time.Now().Add(-10 * time.Minute)
		l.mu.Lock()
		for k, b := range l.buckets {
			if b.last.Before(cutoff) {
				delete(l.buckets, k)
			}
		}
		l.mu.Unlock()
	}
}
