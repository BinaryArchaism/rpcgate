package config_test

import (
	"os"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/BinaryArchaism/rpcgate/internal/config"
)

func Test_ParseConfig(t *testing.T) {
	cfgRaw := `
logger: 
  level: info
  format: json
  out: stdout
`

	path := t.TempDir() + "cfg.yml"
	require.NoError(t, os.WriteFile(path, []byte(cfgRaw), os.ModePerm))
	cfg, err := config.ParseConfig(path)
	require.NoError(t, err)
	require.NotEmpty(t, cfg)
	require.Equal(t, zerolog.InfoLevel, cfg.Logger.Level)
}
