package config

import (
	"os"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
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
	cfg, err := ParseConfig(path)
	require.NoError(t, err)
	require.NotEmpty(t, cfg)
	require.Equal(t, zerolog.InfoLevel, cfg.Logger.Level)
}

func Test_Replace(t *testing.T) {
	t.Setenv("test_env", "test")
	cfgRaw := `
logger: 
#  level: ${test_env}
  format: json
  out: stdout # ${test_env}
  smth: ${test_env}
  one: more
`
	replaced := replacePlaceholdersWithEnv([]byte(cfgRaw))
	require.Equal(t, []byte(`
logger: 
#  level: test
  format: json
  out: stdout # test
  smth: test
  one: more
`), replaced)
}
