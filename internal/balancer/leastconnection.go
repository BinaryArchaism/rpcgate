package balancer

import (
	"math/rand/v2"
	"sync/atomic"
	"time"
)

// LeastConnection implements a least-connections load balancer.
// It tracks the number of in-flight requests per provider and
// prefers providers with fewer active requests.
type LeastConnection struct {
	providers []*LCProvider
}

// NewLeastConnection returns a new LeastConnection balancer.
//
// The passed slice of Payload is copied, so it is safe to modify
// the original slice after calling this function.
func NewLeastConnection(providers []Payload) *LeastConnection {
	p := make([]*LCProvider, 0, len(providers))
	for _, pr := range providers {
		p = append(p, &LCProvider{
			Payload: pr,
		})
	}
	return &LeastConnection{
		providers: p,
	}
}

// LCProvider wraps a Payload and keeps track of in-flight requests.
type LCProvider struct {
	Payload Payload

	inFlight int64
}

// Borrow returns provider payload with least request in flight and release function.
//
// The release callback MUST be called when the request is finished
// to correctly decrement the in-flight counter.
func (lc *LeastConnection) Borrow() (Payload, Release) {
	p := lc.pickLeast()
	if p == nil {
		return Payload{}, func(bool, time.Duration) {}
	}

	p.inFlightInc()
	return p.Payload, func(bool, time.Duration) {
		p.inFlightDec()
	}
}

// pickLeast returns provider with least request in flight.
func (lc *LeastConnection) pickLeast() *LCProvider {
	n := len(lc.providers)
	if n == 0 {
		return nil
	}
	if n == 1 {
		return lc.providers[0]
	}

	minProvider := lc.providers[rand.IntN(len(lc.providers))] //nolint:gosec // unnecessary
	minInFlight := minProvider.loadInFlight()

	for _, p := range lc.providers {
		inFlight := p.loadInFlight()
		if inFlight < minInFlight {
			minProvider = p
			minInFlight = inFlight
		}
	}
	return minProvider
}

// inFlightInc increments the in-flight counter.
func (p *LCProvider) inFlightInc() {
	atomic.AddInt64(&p.inFlight, 1)
}

// inFlightDec decrements the in-flight counter.
func (p *LCProvider) inFlightDec() {
	atomic.AddInt64(&p.inFlight, -1)
}

// loadInFlight loads atomic inFlight var.
func (p *LCProvider) loadInFlight() int64 {
	return atomic.LoadInt64(&p.inFlight)
}
