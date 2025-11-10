package balancer

import (
	"sync"
)

// RoundRobin implements a simple round-robin load-balancing algorithm
// over a static list of providers (Payloads).
type RoundRobin struct {
	payload   []Payload
	currentIX int
	mutex     sync.Mutex
}

// NewRoundRobin returns a new RoundRobin instance.
// The provided slice is copied so it can be safely modified later.
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

// Next returns the next Payload in sequence and advances the index.
// The sequence wraps around to the beginning once it reaches the end.
func (rr *RoundRobin) Next() Payload {
	rr.mutex.Lock()
	defer rr.mutex.Unlock()

	payload := rr.payload[rr.currentIX]
	rr.currentIX++
	if rr.currentIX == len(rr.payload) {
		rr.currentIX = 0
	}

	return payload
}
