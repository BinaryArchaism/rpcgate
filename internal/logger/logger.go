package logger

import (
	"io"
	"os"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/BinaryArchaism/rpcgate/internal/config"
)

func SetupLogger(cfg config.Config) {
	zerolog.SetGlobalLevel(cfg.Logger.Level)
	writer := getLogWriter(cfg)

	logger := zerolog.New(writer).With().Timestamp()
	if cfg.Logger.Level <= zerolog.DebugLevel {
		logger = logger.Caller()
	}
	log.Logger = logger.Logger().Level(cfg.Logger.Level) //nolint:reassign // logger setup
	zerolog.DefaultContextLogger = &log.Logger           //nolint:reassign // logger setup
}

func getLogWriter(cfg config.Config) io.Writer {
	if cfg.Logger.Writer == "none" {
		return io.Discard
	}

	var writer io.Writer = os.Stdout
	if cfg.Logger.Format != "json" {
		writer = zerolog.ConsoleWriter{ //nolint:exhaustruct // logger setup
			Out:        writer,
			NoColor:    cfg.Logger.NoColor,
			TimeFormat: time.RFC3339,
		}
	}

	return writer
}
