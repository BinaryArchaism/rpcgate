package config

import (
	"fmt"
	"os"

	"github.com/goccy/go-yaml"
)

type Config struct {
	Items []Item `yaml:"items"`
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

	return cfg, nil
}
