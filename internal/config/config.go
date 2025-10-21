package config

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/rs/zerolog"
)

const defaultPort = ":8080"

type Config struct {
	NoRequestValidation bool   `yaml:"no_request_validation"`
	Logger              Logger `yaml:"logger"`
	RPCs                []RPC  `yaml:"rpcs"`
	Port                string `yaml:"port"`
}

type Logger struct {
	Level   zerolog.Level `yaml:"level"`
	Format  string        `yaml:"format"`
	Writer  string        `yaml:"writer"`
	NoColor bool          `yaml:"no_color"`
}

type RPC struct {
	Name      string     `yaml:"name"`
	ChainID   int64      `yaml:"chain_id"`
	Algo      string     `yaml:"algo"`
	Providers []Provider `yaml:"providers"`
}

type Provider struct {
	Name    string `yaml:"name"`
	ConnURL string `yaml:"conn_url"`
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

	if cfg.Port == "" {
		cfg.Port = defaultPort
	} else if !strings.HasPrefix(cfg.Port, ":") {
		cfg.Port = ":" + cfg.Port
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
