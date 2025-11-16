package balancer

import (
	"math"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"
)

// P2CEWMA implements the “power of two choices” load balancer
// with EWMA latency, in-flight load and error penalties.
type P2CEWMA struct {
	smooth         float64
	loadNormalizer float64
	penaltyDecay   float64
	cooldown       time.Duration

	providers []*Provider
}

// NewP2CEWMADefault constructs a P2CEWMA with default parameters.
func NewP2CEWMADefault(providers []Payload) *P2CEWMA {
	const (
		smooth         = 0.3
		loadNormalizer = 8
		penaltyDecay   = 0.8
		cooldown       = 10 * time.Second
	)
	return NewP2CEWMA(providers, smooth, loadNormalizer, penaltyDecay, cooldown)
}

// NewP2CEWMA constructs a P2CEWMA with passed parameters.
//
// The passed slice of Payload is copied, so it is safe to modify
// the original slice after calling this function.
func NewP2CEWMA(
	providers []Payload,
	smooth, loadNormalizer, penaltyDecay float64,
	cooldown time.Duration,
) *P2CEWMA {
	p := make([]*Provider, 0, len(providers))
	for _, pr := range providers {
		p = append(p, &Provider{
			Payload: pr,
		})
	}
	return &P2CEWMA{
		smooth:         smooth,
		loadNormalizer: loadNormalizer,
		penaltyDecay:   penaltyDecay,
		cooldown:       cooldown,
		providers:      p,
	}
}

// Borrow picks a provider and returns its Payload plus a release callback.
// You MUST call release(ok, latency) after the upstream request completes,
// where ok indicates provider-level success and latency is the end-to-end duration.
func (b *P2CEWMA) Borrow() (Payload, Release) {
	provider := b.p2c()

	if provider == nil {
		return Payload{}, func(bool, time.Duration) {}
	}

	provider.inFlightInc()
	return provider.Payload, func(ok bool, d time.Duration) {
		provider.onRelease(ok, d, b.smooth, b.penaltyDecay, b.cooldown)
		provider.inFlightDec()
	}
}

// p2c (“power of two choices”): pick two random providers and return the one with the lower score.
func (b *P2CEWMA) p2c() *Provider {
	n := len(b.providers)
	if n == 0 {
		return nil
	}
	if n == 1 {
		return b.providers[0]
	}

	i := rand.IntN(n)     //nolint:gosec // unnecessary
	j := rand.IntN(n - 1) //nolint:gosec // unnecessary
	if i == j {
		j++
	}

	now := time.Now()
	pi, pj := b.providers[i], b.providers[j]

	si := pi.score(now, b.loadNormalizer)
	sj := pj.score(now, b.loadNormalizer)

	if si < sj {
		return pi
	}
	return pj
}

// Provider represents an upstream RPC provider with metadata (Payload)
// and runtime stats used by the balancer.
type Provider struct {
	Payload Payload

	mutex          sync.Mutex
	ewmaMS         float64
	penalty        float64
	unhealthyUntil time.Time

	inFlight int64
}

// score computes a lower-is-better score from EWMA latency, current in-flight load,
// and an error penalty. Returns +Inf while the provider is in cooldown.
func (p *Provider) score(now time.Time, loadNormalizer float64) float64 {
	const baseEWMA = 75

	p.mutex.Lock()
	base := p.ewmaMS
	pen := p.penalty
	until := p.unhealthyUntil
	p.mutex.Unlock()

	if now.Before(until) {
		return math.Inf(1)
	}

	if base == 0 {
		base = baseEWMA
	}

	inFlight := atomic.LoadInt64(&p.inFlight)
	reqLoad := 1 + float64(inFlight)/loadNormalizer

	return base * reqLoad * (1 + pen)
}

// onRelease updates EWMA latency (ms), decays or sets the error penalty,
// and applies cooldown on provider-level failures.
func (p *Provider) onRelease(
	ok bool,
	lat time.Duration,
	alpha, penaltyDecay float64,
	cooldown time.Duration,
) {
	const (
		penaltyValue     = 0.5
		penaltyLostValue = 0.05
	)

	p.mutex.Lock()
	defer p.mutex.Unlock()

	if p.ewmaMS == 0 {
		p.ewmaMS = float64(lat.Milliseconds())
	}

	p.ewmaMS = (1-alpha)*p.ewmaMS + float64(lat.Milliseconds())*alpha

	if !ok {
		p.penalty = penaltyValue
		p.unhealthyUntil = time.Now().Add(cooldown)
	} else {
		p.penalty *= penaltyDecay
		if p.penalty < penaltyLostValue {
			p.penalty = 0
		}
	}
}

// inFlightInc increments the in-flight counter.
func (p *Provider) inFlightInc() {
	atomic.AddInt64(&p.inFlight, 1)
}

// inFlightDec decrements the in-flight counter.
func (p *Provider) inFlightDec() {
	atomic.AddInt64(&p.inFlight, -1)
}
