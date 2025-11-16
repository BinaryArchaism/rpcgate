package balancer

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_LeastConnection(t *testing.T) {
	t.Run("nil providers", func(t *testing.T) {
		lc := NewLeastConnection(nil)
		require.NotNil(t, lc)
		p, _ := lc.Borrow()
		require.Empty(t, p)
	})
	t.Run("one provider", func(t *testing.T) {
		payload := []Payload{
			{
				URL: "first",
			},
		}
		lc := NewLeastConnection(payload)
		require.NotNil(t, lc)
		p1, _ := lc.Borrow()
		p2, _ := lc.Borrow()
		require.Equal(t, p1, p2)
	})
	t.Run("two providers", func(t *testing.T) {
		payload := []Payload{
			{
				URL: "first",
			},
			{
				URL: "second",
			},
		}
		lc := NewLeastConnection(payload)
		require.NotNil(t, lc)

		p1, r1 := lc.Borrow()
		p2, r2 := lc.Borrow()
		require.NotEqual(t, p1.URL, p2.URL)
		r1(true, 0)
		p3, _ := lc.Borrow()
		require.Equal(t, p3.URL, p1.URL)
		r2(true, 0)
		p4, _ := lc.Borrow()
		require.Equal(t, p4.URL, p2.URL)
	})
}
