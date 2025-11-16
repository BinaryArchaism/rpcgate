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

	gotPayload, _ := rr.Borrow()
	require.Equal(t, payload[0], gotPayload)
	gotPayload, _ = rr.Borrow()
	require.Equal(t, payload[1], gotPayload)
	gotPayload, _ = rr.Borrow()
	require.Equal(t, payload[2], gotPayload)
	gotPayload, _ = rr.Borrow()
	require.Equal(t, payload[0], gotPayload)
}
