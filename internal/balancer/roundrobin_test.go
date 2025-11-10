package balancer

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_RoundRobin(t *testing.T) {
	payload := []Payload{
		{
			URL: "first",
		},
		{
			URL: "second",
		},
		{
			URL: "third",
		},
	}
	rr := NewRoundRobin(payload)
	require.NotNil(t, rr)

	require.Equal(t, payload[0], rr.Next())
	require.Equal(t, payload[1], rr.Next())
	require.Equal(t, payload[2], rr.Next())
	require.Equal(t, payload[0], rr.Next())
}
