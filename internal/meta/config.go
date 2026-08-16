// Package meta loads plinth.yml — the only non-query configuration.
package meta

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Database struct {
		URL string `yaml:"url"` // must be a read-only role
	} `yaml:"database"`
	Auth struct {
		Tokens map[string]string `yaml:"tokens"` // caller name -> token (env-expanded)
	} `yaml:"auth"`
	Engine struct {
		DefaultTimeoutMs int `yaml:"default_timeout_ms"`
		MaxRows          int `yaml:"max_rows"`
	} `yaml:"engine"`
	Audit struct {
		Path       string   `yaml:"path"`
		MaskParams []string `yaml:"mask_params"`
	} `yaml:"audit"`
	Semantics struct {
		PullCommand string `yaml:"pull_command"`
	} `yaml:"semantics"`
}

// LoadConfig reads plinth.yml and applies defaults:
// timeout 5000ms, max rows 10000, audit path audit/executions.jsonl.
// ${VAR} in database.url and token values is expanded from the environment.
func LoadConfig(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("plinth.yml: %w", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return nil, fmt.Errorf("plinth.yml: %w", err)
	}
	cfg.Database.URL = os.ExpandEnv(cfg.Database.URL)
	for k, v := range cfg.Auth.Tokens {
		cfg.Auth.Tokens[k] = os.ExpandEnv(v)
	}
	if cfg.Engine.DefaultTimeoutMs <= 0 {
		cfg.Engine.DefaultTimeoutMs = 5000
	}
	if cfg.Engine.MaxRows <= 0 {
		cfg.Engine.MaxRows = 10000
	}
	if cfg.Audit.Path == "" {
		cfg.Audit.Path = "audit/executions.jsonl"
	}
	return &cfg, nil
}
