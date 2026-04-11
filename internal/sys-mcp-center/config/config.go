// Package config holds the configuration structure and loader for sys-mcp-center.
package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// CenterConfig is the root configuration for sys-mcp-center.
type CenterConfig struct {
	Listen  Listen  `yaml:"listen"`
	Auth    Auth    `yaml:"auth"`
	Router  Router  `yaml:"router"`
	Logging Logging `yaml:"logging"`
}

// Listen describes the network addresses for center.
type Listen struct {
	HTTPAddress string `yaml:"http_address"` // MCP/HTTP server, e.g. ":8080"
	GRPCAddress string `yaml:"grpc_address"` // tunnel gRPC server, e.g. ":9090"
	TLS         TLS    `yaml:"tls"`
}

// TLS holds paths to TLS material.
type TLS struct {
	CertFile string `yaml:"cert_file"`
	KeyFile  string `yaml:"key_file"`
	CAFile   string `yaml:"ca_file"`
}

// Auth holds authentication tokens.
type Auth struct {
	// ClientTokens are accepted for MCP HTTP connections (AI clients).
	ClientTokens []string `yaml:"client_tokens"`
	// AgentTokens are accepted for gRPC tunnel connections (agents/proxies).
	AgentTokens []string `yaml:"agent_tokens"`
}

// Router holds routing-related config.
type Router struct {
	// RequestTimeoutSec is the max seconds to wait for a tool response. Default: 5.
	RequestTimeoutSec int `yaml:"request_timeout_sec"`
}

// Logging configures the logger.
type Logging struct {
	Level  string `yaml:"level"`  // debug/info/warn/error. Default: info
	Format string `yaml:"format"` // json/text. Default: json
}

// Load reads the YAML file at path and returns a validated CenterConfig.
func Load(path string) (*CenterConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("center config: read %s: %w", path, err)
	}
	var cfg CenterConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("center config: parse %s: %w", path, err)
	}
	applyDefaults(&cfg)
	if err := validate(&cfg); err != nil {
		return nil, fmt.Errorf("center config: %w", err)
	}
	return &cfg, nil
}

func applyDefaults(cfg *CenterConfig) {
	if cfg.Listen.HTTPAddress == "" {
		cfg.Listen.HTTPAddress = ":8080"
	}
	if cfg.Listen.GRPCAddress == "" {
		cfg.Listen.GRPCAddress = ":9090"
	}
	if cfg.Router.RequestTimeoutSec <= 0 {
		cfg.Router.RequestTimeoutSec = 5
	}
	if cfg.Logging.Level == "" {
		cfg.Logging.Level = "info"
	}
	if cfg.Logging.Format == "" {
		cfg.Logging.Format = "json"
	}
}

func validate(cfg *CenterConfig) error {
	if len(cfg.Auth.AgentTokens) == 0 {
		return fmt.Errorf("auth.agent_tokens must have at least one token")
	}
	return nil
}
