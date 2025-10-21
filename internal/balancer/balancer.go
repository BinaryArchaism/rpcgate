package balancer

import "container/ring"

type RoundRobin struct {
	r *ring.Ring
}

func NewRoundRobin(urls []string) *RoundRobin {
	r := ring.New(len(urls))
	for _, url := range urls {
		r.Value = url
		r = r.Next()
	}
	return &RoundRobin{
		r: r,
	}
}

func (rr *RoundRobin) Next() string {
	rr.r = rr.r.Next()
	return rr.r.Value.(string) //nolint:errcheck // imposible
}
