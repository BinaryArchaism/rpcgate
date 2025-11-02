package metrics

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog/log"

	"github.com/BinaryArchaism/rpcgate/internal/config"
)

type Server struct {
	srv *http.Server
}

func New(cfg config.Config) *Server {
	reg := prometheus.NewRegistry()
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)

	m := http.NewServeMux()

	path := "/metrics"
	if cfg.Metrics.Path != "" {
		path = "/" + strings.TrimPrefix(cfg.Metrics.Path, "/")
	}
	m.Handle(path, promhttp.HandlerFor(
		reg,
		promhttp.HandlerOpts{
			ErrorLog:          &promLogger{},
			EnableOpenMetrics: true,
		},
	))

	var port int64 = 8080
	if cfg.Metrics.Port != 0 {
		port = cfg.Metrics.Port
	}

	return &Server{
		srv: &http.Server{
			Addr:    fmt.Sprintf(":%d", port),
			Handler: m,
		},
	}
}

func (s *Server) Start(ctx context.Context) {
	go func() {
		err := s.srv.ListenAndServe()
		if err != nil {
			if !errors.Is(err, http.ErrServerClosed) {
				log.Ctx(ctx).Panic().Err(err).Msg("Metrics server failed to start")
			}
		}
	}()
	log.Ctx(ctx).Info().Msg("Metrics server started")
}

func (s *Server) Stop() {
	err := s.srv.Shutdown(context.Background())
	if err != nil {
		log.Panic().Err(err).Msg("Metrics server failed to stop")
	}
	log.Info().Msg("Metrics server stopped")
}

type promLogger struct{}

func (promLogger) Println(v ...interface{}) {
	log.Error().Any("err", v).Msg("prometheus error")
}

var _ promhttp.Logger = new(promLogger)
