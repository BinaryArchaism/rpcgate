package startstop

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

const shutdownTimeout = 5 * time.Second

type StartStop interface {
	Start(ctx context.Context)
	Stop()
}

func RunGracefull(ctx context.Context, srvs ...StartStop) {
	log.Info().Msg("Starting application")
	for _, srv := range srvs {
		go srv.Start(ctx)
	}

	<-ctx.Done()
	log.Info().Msg("Stoping application")
	timer := time.Tick(shutdownTimeout)
	wg := sync.WaitGroup{}
	for _, srv := range srvs {
		wg.Go(srv.Stop)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-timer:
		log.Error().Msg("Application stopped before all goroutines done")
	case <-done:
		log.Info().Msg("Application stopped")
	}
}
