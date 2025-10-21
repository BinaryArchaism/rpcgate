package main

import (
	"context"
	"flag"
	"os/signal"
	"syscall"

	"github.com/rs/zerolog/log"

	"github.com/BinaryArchaism/rpcgate/internal/config"
	"github.com/BinaryArchaism/rpcgate/internal/logger"
	"github.com/BinaryArchaism/rpcgate/internal/proxy"
	"github.com/BinaryArchaism/rpcgate/internal/startstop"
)

func main() {
	path := flag.String("path", "~/.config/rpcgate.yaml", "Path to config")

	flag.Parse()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	cfg, err := config.ParseConfig(*path)
	if err != nil {
		log.Panic().Err(err).Str("path", *path).Msg("Failed to parse config")
	}
	logger.SetupLogger(cfg)

	srv := proxy.New(cfg)

	startstop.RunGracefull(ctx, srv)
}
