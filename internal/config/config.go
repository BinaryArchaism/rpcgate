package config

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/goccy/go-yaml"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

const (
	defaultPort       = ":8080"
	deafultConfigPath = "/.config/rpcgate/rpcgate.yaml"
)

type Config struct {
	NoRPCValidation bool          `yaml:"no_rpc_validation"`
	P2CEWMA         P2CEWMAConfig `yaml:"p2cewma"`
	Clients         Clients       `yaml:"clients"`
	Logger          Logger        `yaml:"logger"`
	Metrics         Metrics       `yaml:"metrics"`
	RPCs            []RPC         `yaml:"rpcs"`
	Port            string        `yaml:"port"`
}

type Metrics struct {
	Enabled bool   `yaml:"enabled"`
	Port    int64  `yaml:"port"`
	Path    string `yaml:"path"`
}

type Clients struct {
	AuthRequired bool     `yaml:"auth_required"`
	Type         string   `yaml:"type"`
	Clients      []Client `yaml:"clients"`
}

type Client struct {
	Login    string `yaml:"login"`
	Password string `yaml:"password"`
}

type Logger struct {
	Level   zerolog.Level `yaml:"level"`
	Format  string        `yaml:"format"`
	Writer  string        `yaml:"writer"`
	NoColor bool          `yaml:"no_color"`
}

type RPC struct {
	Name         string        `yaml:"name"`
	ChainID      int64         `yaml:"chain_id"`
	BalancerType string        `yaml:"balancer_type"`
	P2CEWMA      P2CEWMAConfig `yaml:"p2cewma"`
	Providers    []Provider    `yaml:"providers"`
}

type Provider struct {
	Name    string `yaml:"name"`
	ConnURL string `yaml:"conn_url"`
}

type P2CEWMAConfig struct {
	Smooth          float64       `yaml:"smooth"`
	LoadNormalizer  float64       `yaml:"load_normalizer"`
	PenaltyDecay    float64       `yaml:"penalty_decay"`
	CooldownTimeout time.Duration `yaml:"cooldown_timeout"`
}

func ParseConfig(path string) (Config, error) {
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return Config{}, fmt.Errorf("can not get user home dit: %w", err)
		}
		path = home + deafultConfigPath
	}
	var cfg Config
	ymlBytes, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("can not read yaml config file: %w", err)
	}
	yml := string(ymlBytes)
	placeholderToValue := parseConfigPlaceholders(yml)
	for placeholder, value := range placeholderToValue {
		yml = strings.ReplaceAll(yml, placeholder, value)
	}

	err = yaml.Unmarshal([]byte(yml), &cfg)
	if err != nil {
		return Config{}, fmt.Errorf("can not unmarshal yaml config file: %w", err)
	}

	cfg.Port = getPort(cfg.Port)

	err = validateConfig(cfg)
	if err != nil {
		return Config{}, fmt.Errorf("can not validate config file: %w", err)
	}

	if !cfg.NoRPCValidation {
		err = validateRPCs(cfg.RPCs)
		if err != nil {
			return Config{}, fmt.Errorf("can not validate rpcs: %w", err)
		}
	}

	return cfg, nil
}

func getPort(port string) string {
	if port == "" {
		return defaultPort
	}
	if !strings.HasPrefix(port, ":") {
		port = ":" + port
	}

	return port
}

//nolint:cyclop // validation of config
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
	switch cfg.Clients.Type {
	case "", "basic", "query":
	default:
		return errors.New("Clients.Type incorrect, should be on of 'basic', 'query' or empty")
	}

	for i, rpc := range cfg.RPCs {
		switch rpc.BalancerType {
		case "":
			cfg.RPCs[i].BalancerType = "p2cewma"
		case "round-robin", "p2cewma":
		default:
			return fmt.Errorf(
				"RPC[%s].BalancerType incorrect, should be on of 'round-robin', 'p2cewma' or empty",
				rpc.Name,
			)
		}
	}

	err := validateP2CEWMA(cfg.P2CEWMA)
	if err != nil {
		return fmt.Errorf("global p2cewma config is invalid: %w", err)
	}

	for _, rpc := range cfg.RPCs {
		err = validateP2CEWMA(rpc.P2CEWMA)
		if err != nil {
			return fmt.Errorf("RPC[%s].P2CEWMA config is invalid: %w", rpc.Name, err)
		}
	}

	return nil
}

func validateP2CEWMA(cfg P2CEWMAConfig) error {
	if cfg.Smooth < 0 || cfg.Smooth > 1 {
		return fmt.Errorf("P2CEWMAConfig.Smooth incorrect, should be [0;1], got: %f", cfg.Smooth)
	}
	if cfg.PenaltyDecay < 0 || cfg.PenaltyDecay > 1 {
		return fmt.Errorf("P2CEWMAConfig.PenaltyDecay incorrect, should be [0;1], got: %f", cfg.PenaltyDecay)
	}
	if cfg.LoadNormalizer < 0 {
		return fmt.Errorf("P2CEWMAConfig.LoadNormalizer incorrect, should be > 0, got: %f", cfg.LoadNormalizer)
	}

	return nil
}

func validateRPCs(rpcs []RPC) error {
	for _, rpc := range rpcs {
		for _, provider := range rpc.Providers {
			cli, err := ethclient.Dial(provider.ConnURL)
			if err != nil {
				return fmt.Errorf("can not dial provider '%s' for chain '%d'", provider.Name, rpc.ChainID)
			}
			chainID, err := cli.ChainID(context.Background())
			if err != nil {
				return fmt.Errorf("can not get chainID for provider '%s' for chain '%d', err: %w",
					provider.Name, rpc.ChainID, err)
			}
			if chainID.Int64() != rpc.ChainID {
				return fmt.Errorf("chainID mismatched for provider '%s' for chain '%d', got: %d",
					provider.Name, rpc.ChainID, chainID.Int64())
			}
		}
	}

	return nil
}

func parseConfigPlaceholders(rawCfg string) map[string]string {
	re := regexp.MustCompile(`\$\{[^}]+\}`)
	placeholders := re.FindAllString(rawCfg, -1)
	result := make(map[string]string)
	for _, ph := range placeholders {
		key := strings.TrimSuffix(strings.TrimPrefix(ph, "${"), "}")
		value, found := os.LookupEnv(key)
		if !found {
			log.Panic().Str("key", key).Msg("can not find env by key")
		}
		result[ph] = value
	}

	return result
}
