package config

import (
	"errors"
	"fmt"
	"os"

	"github.com/goccy/go-yaml"
	"github.com/rs/zerolog"
)

type Config struct {
	Logger Logger `yaml:"logger"`
	Items  []Item `yaml:"items"`
}

type Logger struct {
	Level   zerolog.Level `yaml:"level"`
	Format  string        `yaml:"format"`
	Writer  string        `yaml:"writer"`
	NoColor bool          `yaml:"no_color"`
}

type Item struct {
	Name string `yaml:"name"`
}

type RPCConn struct {
	RPCURL string `yaml:"rpc_url"`
}

func ParseConfig(path string) (Config, error) {
	var cfg Config
	yml, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("can not read yaml config file: %w", err)
	}
	err = yaml.Unmarshal(yml, &cfg)
	if err != nil {
		return Config{}, fmt.Errorf("can not unmarshal yaml config file: %w", err)
	}
	err = validateConfig(cfg)
	if err != nil {
		return Config{}, fmt.Errorf("can not validate config file: %w", err)
	}

	return cfg, nil
}

func validateConfig(cfg Config) error {
	switch cfg.Logger.Format {
	case "", "json", "inline":
	default:
		return errors.New("Logger.Format incorrect, should be on of 'json', 'inline' or empty")
	}
	switch cfg.Logger.Writer {
	case "", "stdout", "none":
	default:
		return errors.New("Logger.Writer incorrect, should be on of 'stdout', 'none' or empty")
	}

	return nil
}
