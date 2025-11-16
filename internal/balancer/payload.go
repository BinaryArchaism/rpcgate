package balancer

import "time"

type Release func(success bool, latency time.Duration)

// Payload holds provider metadata used by load balancers.
type Payload struct {
	URL  string
	Name string
}
