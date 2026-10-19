package gateway

import (
	"sync"
	"time"
)

// Circuit breaker: after this many consecutive endpoint failures, the
// endpoint is skipped for a backoff that doubles up to circuitMax.
const (
	circuitThreshold = 3
	circuitBase      = 30 * time.Second
	circuitMax       = 5 * time.Minute
	defaultCooldown  = time.Minute // provider key cooldown after a 429 without Retry-After
	maxCooldown      = 10 * time.Minute
)

// health is the gateway's in-memory view of upstream health: provider key
// cooldowns, endpoint circuit breakers and round-robin positions. It is per
// process, which suits a single-server deployment.
type health struct {
	mu       sync.Mutex
	cooldown map[int64]time.Time // provider key id → usable again at
	circuits map[int64]*circuit  // endpoint id
	rr       map[int64]int       // provider id → next key offset
	now      func() time.Time
}

type circuit struct {
	failures  int
	openUntil time.Time
	backoff   time.Duration
}

func newHealth() *health {
	return &health{cooldown: map[int64]time.Time{}, circuits: map[int64]*circuit{}, rr: map[int64]int{}, now: time.Now}
}

func (h *health) coolKey(id int64, d time.Duration) {
	if d <= 0 {
		d = defaultCooldown
	}
	h.mu.Lock()
	h.cooldown[id] = h.now().Add(min(d, maxCooldown))
	h.mu.Unlock()
}

// keyCoolingUntil reports when a cooling-down key becomes usable again.
func (h *health) keyCoolingUntil(id int64) (time.Time, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	t, ok := h.cooldown[id]
	if ok && !t.After(h.now()) {
		delete(h.cooldown, id)
		return time.Time{}, false
	}
	return t, ok
}

func (h *health) clearKey(id int64) {
	h.mu.Lock()
	delete(h.cooldown, id)
	h.mu.Unlock()
}

func (h *health) endpointOK(id int64) {
	h.mu.Lock()
	delete(h.circuits, id)
	h.mu.Unlock()
}

func (h *health) endpointFailed(id int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	c := h.circuits[id]
	if c == nil {
		c = &circuit{}
		h.circuits[id] = c
	}
	c.failures++
	if c.failures >= circuitThreshold {
		if c.backoff == 0 {
			c.backoff = circuitBase
		} else {
			c.backoff = min(c.backoff*2, circuitMax)
		}
		c.openUntil = h.now().Add(c.backoff)
		c.failures = 0
	}
}

// endpointOpenUntil reports when an open circuit closes again.
func (h *health) endpointOpenUntil(id int64) (time.Time, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	c := h.circuits[id]
	if c == nil || !c.openUntil.After(h.now()) {
		return time.Time{}, false
	}
	return c.openUntil, true
}

// order moves endpoints with an open circuit to the back, keeping the
// relative order otherwise; they are still tried if nothing else works.
func (h *health) order(cands []candidate) []candidate {
	out := make([]candidate, 0, len(cands))
	var open []candidate
	for _, c := range cands {
		if _, isOpen := h.endpointOpenUntil(c.endpoint.ID); isOpen {
			open = append(open, c)
		} else {
			out = append(out, c)
		}
	}
	return append(out, open...)
}

// rotate returns the starting offset for a provider's n keys and advances it.
func (h *health) rotate(providerID int64, n int) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	off := h.rr[providerID] % n
	h.rr[providerID] = off + 1
	return off
}

// rateLimiter enforces requests-per-minute limits with a sliding window.
type rateLimiter struct {
	mu   sync.Mutex
	hits map[string][]time.Time
	now  func() time.Time
}

func newRateLimiter() *rateLimiter {
	return &rateLimiter{hits: map[string][]time.Time{}, now: time.Now}
}

type limit struct {
	key string
	rpm int
}

// allow admits a request only if every limit has room, and then counts it
// against all of them. On refusal it returns how long until a slot frees up.
func (l *rateLimiter) allow(limits ...limit) (bool, time.Duration) {
	now := l.now()
	cutoff := now.Add(-time.Minute)
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.hits) > 50000 {
		for k, ts := range l.hits {
			if len(ts) == 0 || !ts[len(ts)-1].After(cutoff) {
				delete(l.hits, k)
			}
		}
	}
	var wait time.Duration
	for _, lim := range limits {
		ts := l.hits[lim.key]
		i := 0
		for i < len(ts) && !ts[i].After(cutoff) {
			i++
		}
		ts = ts[i:]
		l.hits[lim.key] = ts
		if len(ts) >= lim.rpm {
			wait = max(wait, ts[len(ts)-lim.rpm].Add(time.Minute).Sub(now))
		}
	}
	if wait > 0 {
		return false, wait
	}
	for _, lim := range limits {
		l.hits[lim.key] = append(l.hits[lim.key], now)
	}
	return true, 0
}
