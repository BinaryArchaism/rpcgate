package balancer

import (
	"container/ring"

	"github.com/BinaryArchaism/rpcgate/internal/config"
)

type RoundRobin struct {
	r *ring.Ring
}

func NewRoundRobin(urls []config.Provider) *RoundRobin {
	r := ring.New(len(urls))
	for _, url := range urls {
		r.Value = url
		r = r.Next()
	}
	return &RoundRobin{
		r: r,
	}
}

func (rr *RoundRobin) Next() config.Provider {
	rr.r = rr.r.Next()
	return rr.r.Value.(config.Provider) //nolint:errcheck // imposible
}
