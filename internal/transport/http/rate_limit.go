package httptransport

import (
	"math"
	"net/netip"
	"sync"
	"time"
)

const clientIdleTTL = 5 * time.Minute
const cleanupInterval = time.Minute

type bucket struct {
	tokens   float64
	updated  time.Time
	lastSeen time.Time
}

type ipLimiter struct {
	mu          sync.Mutex
	clients     map[netip.Addr]bucket
	rps         float64
	burst       float64
	maxClients  int
	lastCleanup time.Time
	now         func() time.Time
}

func newIPLimiter(rps, burst, maxClients int) *ipLimiter {
	return &ipLimiter{clients: make(map[netip.Addr]bucket), rps: float64(rps), burst: float64(burst), maxClients: maxClients, now: time.Now}
}

// allow возвращает 0 при допуске, иначе время ожидания в целых секундах.
func (l *ipLimiter) allow(ip netip.Addr) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if l.lastCleanup.IsZero() || now.Sub(l.lastCleanup) >= cleanupInterval {
		l.cleanup(now)
	}
	b, exists := l.clients[ip]
	if !exists {
		if len(l.clients) >= l.maxClients {
			l.cleanup(now)
			if len(l.clients) >= l.maxClients {
				return 60
			}
		}
		b = bucket{tokens: l.burst, updated: now}
	}
	// Время берётся под mutex: параллельные запросы не возвращают часы bucket назад.
	elapsed := max(0, now.Sub(b.updated).Seconds())
	b.tokens = min(l.burst, b.tokens+elapsed*l.rps)
	b.updated = now
	b.lastSeen = now
	retry := 0
	if b.tokens >= 1 {
		b.tokens--
	} else {
		retry = max(1, int(math.Ceil((1-b.tokens)/l.rps)))
	}
	l.clients[ip] = b
	return retry
}

func (l *ipLimiter) cleanup(now time.Time) {
	for ip, b := range l.clients {
		if now.Sub(b.lastSeen) >= clientIdleTTL {
			delete(l.clients, ip)
		}
	}
	l.lastCleanup = now
}
