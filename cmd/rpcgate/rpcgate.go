package main

import (
	"context"
	"flag"
	"os/signal"
	"syscall"

	"github.com/rs/zerolog/log"

	"github.com/BinaryArchaism/rpcgate/internal/config"
	"github.com/BinaryArchaism/rpcgate/internal/logger"
	"github.com/BinaryArchaism/rpcgate/internal/metrics"
	"github.com/BinaryArchaism/rpcgate/internal/proxy"
	"github.com/BinaryArchaism/rpcgate/internal/startstop"
)

func main() {
	configPath := flag.String("config", "~/.config/rpcgate.yaml", "Path to config")
	flag.Parse()

	cfg, err := config.ParseConfig(*configPath)
	if err != nil {
		log.Panic().Err(err).Str("config_path", *configPath).Msg("Failed to parse config")
	}
	logger.SetupLogger(cfg)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	var apps []startstop.StartStop

	srv := proxy.New(cfg)
	apps = append(apps, srv)

	if cfg.Metrics.Enabled {
		metricsSrv := metrics.New(cfg)
		apps = append(apps, metricsSrv)
	}

	startstop.RunGracefull(ctx, apps...)
}
