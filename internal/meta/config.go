// Package meta loads plinth.yml — the only non-query configuration.
package meta

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
	"slices"

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
		// single string, split on spaces at use site; no quoted-arg support
		PullCommand string `yaml:"pull_command"`
	} `yaml:"semantics"`
}

// envRef matches only the explicit ${VAR_NAME} form.
var envRef = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// expandEnv substitutes ${VAR_NAME} references from the environment.
// Unset variables expand to the empty string (intentional: an unset secret
// must not silently pass a literal "${...}" through as a token). Every other
// '$' — bare $var, $$, bcrypt-style $2b$... — passes through untouched.
func expandEnv(s string) string {
	return envRef.ReplaceAllStringFunc(s, func(m string) string {
		return os.Getenv(m[2 : len(m)-1])
	})
}

// LoadConfig reads plinth.yml and applies defaults:
// timeout 5000ms, max rows 10000, audit path audit/executions.jsonl.
// ${VAR} in database.url and token values is expanded from the environment.
// Unknown keys are rejected, and two callers may not share a token value.
func LoadConfig(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("plinth.yml: %w", err)
	}
	var cfg Config
	d := yaml.NewDecoder(bytes.NewReader(b))
	d.KnownFields(true) // reject unknown keys
	if err := d.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("plinth.yml: %w", err)
	}
	cfg.Database.URL = expandEnv(cfg.Database.URL)
	for k, v := range cfg.Auth.Tokens {
		cfg.Auth.Tokens[k] = expandEnv(v)
	}
	names := make([]string, 0, len(cfg.Auth.Tokens))
	for name := range cfg.Auth.Tokens {
		names = append(names, name)
	}
	slices.Sort(names)
	firstCaller := make(map[string]string, len(cfg.Auth.Tokens)) // token value -> caller
	for _, name := range names {
		v := cfg.Auth.Tokens[name]
		if first, dup := firstCaller[v]; dup {
			return nil, fmt.Errorf("plinth.yml: duplicate token value for callers %q, %q", first, name)
		}
		firstCaller[v] = name
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
