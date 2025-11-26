package config

import (
	"context"
	"errors"
	"fmt"
	"net/url"
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
	defaultServerPort  = 8080
	defaultMetricsPort = 9090
	defaultMetricsPath = "/metrics"
	defaultConfigPath  = "/.config/rpcgate/rpcgate.yaml"
)

const (
	ewmaSmooth         = 0.3
	ewmaLoadNormalizer = 8
	ewmaPenaltyDecay   = 0.8
	ewmaCooldown       = 10 * time.Second
)

type Config struct {
	GlobalRPCConfig

	Clients Clients `yaml:"clients"`
	Logger  Logger  `yaml:"logger"`
	Metrics Metrics `yaml:"metrics"`
	RPCs    []RPC   `yaml:"rpcs"`
	Port    int64   `yaml:"port"`
}

type GlobalRPCConfig struct {
	BalancerType    string        `yaml:"balancer_type"`
	NoRPCValidation bool          `yaml:"no_rpc_validation"`
	P2CEWMA         P2CEWMAConfig `yaml:"p2cewma"`
}

type Metrics struct {
	Enabled bool   `yaml:"enabled"`
	Port    int64  `yaml:"port"`
	Path    string `yaml:"path"`
}

type Clients struct {
	AuthRequired bool     `yaml:"auth_required"` // only for basic type of auth.
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
	GlobalRPCConfig

	Name      string     `yaml:"name"`
	ChainID   int64      `yaml:"chain_id"`
	Providers []Provider `yaml:"providers"`
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
			return Config{}, fmt.Errorf("can not get user home dir: %w", err)
		}
		path = home + defaultConfigPath
	}
	var cfg Config
	yml, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("can not read yaml config file: %w", err)
	}
	yml = replacePlaceholdersWithEnv(yml)
	err = yaml.Unmarshal(yml, &cfg)
	if err != nil {
		return Config{}, fmt.Errorf("can not unmarshal yaml config file: %w", err)
	}

	cfg.Port = getPort(cfg.Port, defaultServerPort)
	cfg.Metrics.Port = getPort(cfg.Metrics.Port, defaultMetricsPort)
	if cfg.Metrics.Path != "" {
		cfg.Metrics.Path = "/" + strings.TrimPrefix(cfg.Metrics.Path, "/")
	} else {
		cfg.Metrics.Path = defaultMetricsPath
	}

	err = validateConfig(&cfg)
	if err != nil {
		return Config{}, fmt.Errorf("can not validate config file: %w", err)
	}

	return cfg, nil
}

func getPort(port, defaultPort int64) int64 {
	if port == 0 {
		return defaultPort
	}

	return port
}

func validateConfig(cfg *Config) error {
	if err := validateGlobalRPCConfig(&cfg.GlobalRPCConfig); err != nil {
		return fmt.Errorf("global rpc config is invalid: %w", err)
	}
	if err := validateLogger(cfg.Logger); err != nil {
		return fmt.Errorf("logger config is invalid: %w", err)
	}
	if err := validateClients(cfg.Clients); err != nil {
		return fmt.Errorf("clients config is invalid: %w", err)
	}
	if err := validateRPCs(cfg); err != nil {
		return fmt.Errorf("rpc config is invalid: %w", err)
	}
	return nil
}

func validateRPCs(cfg *Config) error {
	var emptyGlobalRPCCfg GlobalRPCConfig
	names := make(map[string]struct{})
	for i, rpc := range cfg.RPCs {
		if len(rpc.Providers) == 0 {
			return fmt.Errorf("rpc[%s].name is not unique", rpc.Name)
		}
		_, exist := names[rpc.Name]
		if exist {
			return fmt.Errorf("rpc[%s].name is not unique", rpc.Name)
		}
		if err := validateRPCsChainID(rpc); err != nil {
			return fmt.Errorf("rpc[%s].chain_id is invalid: %w", rpc.Name, err)
		}
		if rpc.GlobalRPCConfig == emptyGlobalRPCCfg {
			cfg.RPCs[i].GlobalRPCConfig = cfg.GlobalRPCConfig
			continue
		}
		if err := validateGlobalRPCConfig(&rpc.GlobalRPCConfig); err != nil {
			return fmt.Errorf("rpc[%s] config is invalid: %w", rpc.Name, err)
		}
		if err := validateProviderConnURL(rpc); err != nil {
			return fmt.Errorf("rpc[%s] config is invalid: %w", rpc.Name, err)
		}
	}
	return nil
}

func validateProviderConnURL(rpc RPC) error {
	var http, ws int
	for _, provider := range rpc.Providers {
		parsedURL, err := url.Parse(provider.ConnURL)
		if err != nil {
			return fmt.Errorf("rpc[%s].provider[%s].conn_url invalid", rpc.Name, provider.Name)
		}
		switch parsedURL.Scheme {
		case "http", "https":
			http++
		case "ws", "wss":
			if rpc.BalancerType == "" || rpc.BalancerType == "p2cewma" {
				return fmt.Errorf("rpc[%s].balancer_type is unsupported for websocket", rpc.Name)
			}
			ws++
		default:
			return fmt.Errorf(
				"rpc[%s].provider[%s].conn_url scheme invalid: %s",
				rpc.Name,
				provider.Name,
				parsedURL.Scheme,
			)
		}
	}
	if http*ws == 0 {
		return nil
	}
	return fmt.Errorf("rpc[%s] has both http and websocket connections", rpc.Name)
}

func validateGlobalRPCConfig(cfg *GlobalRPCConfig) error {
	switch cfg.BalancerType {
	case "", "p2cewma":
		cfg.BalancerType = "p2cewma"
	case "round-robin", "least-connection":
		return nil
	default:
		return errors.New(
			"balancer_type incorrect, must be one of 'round-robin', 'p2cewma', 'least-connection' or empty",
		)
	}

	isEmpty := cfg.P2CEWMA == P2CEWMAConfig{}
	if isEmpty {
		cfg.P2CEWMA = P2CEWMAConfig{
			Smooth:          ewmaSmooth,
			LoadNormalizer:  ewmaLoadNormalizer,
			PenaltyDecay:    ewmaPenaltyDecay,
			CooldownTimeout: ewmaCooldown,
		}
		return nil
	}

	if cfg.P2CEWMA.Smooth < 0 || cfg.P2CEWMA.Smooth > 1 {
		return fmt.Errorf("p2cewma.smooth incorrect, must be [0;1], got: %f", cfg.P2CEWMA.Smooth)
	}
	if cfg.P2CEWMA.PenaltyDecay < 0 || cfg.P2CEWMA.PenaltyDecay > 1 {
		return fmt.Errorf("p2cewma.penalty_decay incorrect, must be [0;1], got: %f", cfg.P2CEWMA.PenaltyDecay)
	}
	if cfg.P2CEWMA.LoadNormalizer <= 0 {
		return fmt.Errorf("p2cewma.load_normalizer incorrect, must be > 0, got: %f", cfg.P2CEWMA.LoadNormalizer)
	}

	return nil
}

func validateLogger(cfg Logger) error {
	switch cfg.Format {
	case "", "json", "inline":
	default:
		return errors.New("logger.format incorrect, must be on of 'json', 'inline' or empty")
	}
	switch cfg.Writer {
	case "", "stdout", "none":
	default:
		return errors.New("logger.writer incorrect, must be on of 'stdout', 'none' or empty")
	}

	return nil
}

func validateClients(cfg Clients) error {
	switch cfg.Type {
	case "", "basic", "query":
	default:
		return errors.New("clients.type incorrect, must be on of 'basic', 'query' or empty")
	}

	return nil
}

func validateRPCsChainID(rpc RPC) error {
	for _, provider := range rpc.Providers {
		cli, err := ethclient.Dial(provider.ConnURL)
		if err != nil {
			return fmt.Errorf("can not dial provider '%s' for chain '%d'", provider.Name, rpc.ChainID)
		}
		chainID, err := cli.ChainID(context.Background())
		if err != nil {
			return fmt.Errorf("can not get chain_id for provider '%s' for chain '%d', err: %w",
				provider.Name, rpc.ChainID, err)
		}
		if chainID.Int64() != rpc.ChainID {
			return fmt.Errorf("chain_id mismatched for provider '%s' for chain '%d', got: %d",
				provider.Name, rpc.ChainID, chainID.Int64())
		}
		cli.Close()
	}

	return nil
}

func replacePlaceholdersWithEnv(raw []byte) []byte {
	re := regexp.MustCompile(`\$\{([^}]+)\}`)

	return re.ReplaceAllFunc(raw, func(match []byte) []byte {
		// match = ${KEY}
		key := match[2 : len(match)-1]

		val, ok := os.LookupEnv(string(key))
		if !ok {
			log.Panic().Str("key", string(key)).Msg("env not found")
		}
		return []byte(val)
	})
}
