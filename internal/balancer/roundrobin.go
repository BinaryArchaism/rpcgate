package balancer

import (
	"sync"
	"time"
)

// RoundRobin implements a simple round-robin load-balancing algorithm
// over a static list of providers (Payloads).
type RoundRobin struct {
	payload   []Payload
	currentIX int
	mutex     sync.Mutex
}

// NewRoundRobin returns a new RoundRobin instance.
//
// The passed slice of Payload is copied, so it is safe to modify
// the original slice after calling this function.
func NewRoundRobin(urls []Payload) *RoundRobin {
	payload := make([]Payload, 0, len(urls))
	for _, url := range urls {
		payload = append(payload, Payload{
			URL:  url.URL,
			Name: url.Name,
		})
	}
	return &RoundRobin{
		payload: payload,
	}
}

// Borrow returns the next Payload in sequence and advances the index.
// The sequence wraps around to the beginning once it reaches the end.
func (rr *RoundRobin) Borrow() (Payload, Release) {
	rr.mutex.Lock()
	defer rr.mutex.Unlock()

	payload := rr.payload[rr.currentIX]
	rr.currentIX++
	if rr.currentIX == len(rr.payload) {
		rr.currentIX = 0
	}

	return payload, func(bool, time.Duration) {}
}
