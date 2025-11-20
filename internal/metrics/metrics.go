package metrics

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog/log"

	"github.com/BinaryArchaism/rpcgate/internal/config"
)

const (
	namespace      = "rpcgate"
	defaultTimeout = 5 * time.Second
)

//nolint:gochecknoglobals // metrics
var (
	RequestLatencySeconds = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "request_latency_seconds",
		Help:      "Request latency distribution in seconds",
		Buckets:   []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2, 5},
	}, []string{"chain_id", "rpc_name", "provider", "balancer", "method", "client"})
	RequestTotalCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "request_total",
		Help:      "Request total",
	}, []string{"chain_id", "rpc_name", "provider", "balancer", "method", "client"})
	RequestError = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "request_error_total",
		Help:      "Request error total",
	}, []string{"chain_id", "rpc_name", "provider", "balancer", "method", "client"})
	ClientRequestError = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "client_request_error_total",
		Help:      "Client request error total",
	}, []string{"chain_id", "rpc_name", "provider", "balancer", "method", "client"})
	ResponseSizeBytes = prometheus.NewSummaryVec(prometheus.SummaryOpts{
		Namespace: namespace,
		Name:      "response_size_bytes",
		Help:      "Response size bytes gauge",
	}, []string{"chain_id", "rpc_name", "provider", "balancer", "method", "client"})
)

type Server struct {
	srv *http.Server
}

func New(cfg config.Config) *Server {
	reg := prometheus.NewRegistry()
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		RequestLatencySeconds,
		RequestTotalCounter,
		RequestError,
		ClientRequestError,
		ResponseSizeBytes,
	)
	m := http.NewServeMux()

	m.Handle(cfg.Metrics.Path, promhttp.HandlerFor(
		reg,
		promhttp.HandlerOpts{
			ErrorLog:          &promLogger{},
			EnableOpenMetrics: true,
		},
	))
	return &Server{
		srv: &http.Server{
			Addr:              fmt.Sprintf(":%d", cfg.Metrics.Port),
			Handler:           m,
			ReadTimeout:       defaultTimeout,
			ReadHeaderTimeout: defaultTimeout,
			WriteTimeout:      defaultTimeout,
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
