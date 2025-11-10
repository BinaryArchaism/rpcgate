package balancer

import (
	"math"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const delta = 0.000001

func Test_P2CEWMA_NewP2CEWMA(t *testing.T) {
	expected := P2CEWMA{
		smooth:         0.3,
		loadNormalizer: 8,
		penaltyDecay:   0.8,
		cooldown:       10 * time.Second,
		providers:      []*Provider{},
	}
	b := NewP2CEWMADefault(nil)
	require.NotNil(t, b)
	require.Equal(t, expected, *b)
	b = NewP2CEWMA(nil, 0.3, 8, 0.8, 10*time.Second)
	require.NotNil(t, b)
	require.Equal(t, expected, *b)

	b = NewP2CEWMADefault([]Payload{})
	require.NotNil(t, b)
	require.NotNil(t, b.providers)
}

func Test_P2CEWMA_Borrow(t *testing.T) {
	t.Run("empty providers", func(t *testing.T) {
		b := NewP2CEWMADefault(nil)
		require.NotNil(t, b)
		p, _ := b.Borrow()
		require.Empty(t, p)
	})
	t.Run("ok", func(t *testing.T) {
		b := NewP2CEWMADefault([]Payload{{Name: "1"}, {Name: "2"}})
		b.providers[0].ewmaMS = 60
		p1, r := b.Borrow()
		require.NotEmpty(t, p1)
		require.Equal(t, "1", p1.Name)
		require.Equal(t, int64(1), b.providers[0].inFlight)
		r(true, 60*time.Millisecond)
		require.Equal(t, int64(0), b.providers[0].inFlight)
		require.InDelta(t, 60.0, b.providers[0].ewmaMS, delta)
	})
}

func Test_P2CEWMA_p2c(t *testing.T) {
	t.Run("empty providers", func(t *testing.T) {
		b := NewP2CEWMADefault(nil)
		require.Nil(t, b.p2c())
	})
	t.Run("1 provider", func(t *testing.T) {
		b := NewP2CEWMADefault([]Payload{{Name: "test"}})
		require.NotNil(t, b.p2c())
		require.Equal(t, "test", b.p2c().Payload.Name)
	})
	t.Run("ok", func(t *testing.T) {
		b := NewP2CEWMADefault([]Payload{{Name: "1"}, {Name: "2"}})
		b.providers[0].ewmaMS = 60
		p1 := b.p2c()
		require.Equal(t, "1", p1.Payload.Name)
		p1 = b.p2c()
		require.Equal(t, "1", p1.Payload.Name)
		p1.ewmaMS = 100
		p2 := b.p2c()
		require.Equal(t, "2", p2.Payload.Name)
	})
}

func Test_Provider_score(t *testing.T) {
	t.Run("score ok", func(t *testing.T) {
		var p Provider
		require.InDelta(t, 75.0, p.score(time.Now(), 8), delta)
	})
	t.Run("unhealthy endpoint", func(t *testing.T) {
		var p Provider
		p.onRelease(false, time.Duration(75)*time.Millisecond, 0.3, 0.8, 10*time.Second)
		require.InDelta(t, math.Inf(1), p.score(time.Now(), 8), delta)
	})
}

func Test_Provider_onRelease(t *testing.T) {
	t.Run("success stable ms", func(t *testing.T) {
		var p Provider
		for range 10 {
			p.onRelease(true, 75*time.Millisecond, 0.3, 0.8, 10*time.Second)
		}
		require.InDelta(t, 75.0, p.ewmaMS, delta)
	})
	t.Run("success getting higher ms", func(t *testing.T) {
		var p Provider
		for i := range 10 {
			p.onRelease(true, time.Duration(75+i)*time.Millisecond, 0.3, 0.8, 10*time.Second)
		}
		require.Less(t, 75.0, p.ewmaMS)
	})
	t.Run("success getting lower ms", func(t *testing.T) {
		var p Provider
		for i := range 10 {
			p.onRelease(true, time.Duration(75-i)*time.Millisecond, 0.3, 0.8, 10*time.Second)
		}
		require.Greater(t, 75.0, p.ewmaMS)
	})
	t.Run("error and cooldown", func(t *testing.T) {
		var p Provider
		p.onRelease(false, 75*time.Millisecond, 0.3, 0.8, 10*time.Second)
		require.InDelta(t, 0.5, p.penalty, delta)
		require.True(t, time.Now().Before(p.unhealthyUntil))
	})
	t.Run("error penalty decreasing", func(t *testing.T) {
		var p Provider
		p.onRelease(false, 75*time.Millisecond, 0.3, 0.8, 10*time.Second)
		require.InDelta(t, 0.5, p.penalty, delta)
		require.True(t, time.Now().Before(p.unhealthyUntil))
		p.onRelease(true, 75*time.Millisecond, 0.3, 0.8, 10*time.Second)
		require.InDelta(t, 0.8*0.5, p.penalty, delta)
	})
}

func Test_Provider_inFlight(t *testing.T) {
	p := Provider{
		inFlight: 10,
	}
	wg := sync.WaitGroup{}
	for i := range 6 {
		wg.Go(func() {
			for range 1000 {
				if i%2 == 0 {
					p.inFlightInc()
				} else {
					p.inFlightDec()
				}
			}
		})
	}
	wg.Wait()

	require.Equal(t, int64(10), p.inFlight)
}
