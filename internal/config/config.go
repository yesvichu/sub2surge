// Package config holds the server configuration model.
package config

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	defaultListen = "127.0.0.1:8080"
	defaultUA     = "v2rayN/6.31"
	defaultTTL    = time.Hour
)

// Sub is one upstream subscription.
type Sub struct {
	Name     string `json:"name"`
	URL      string `json:"url"`
	UA       string `json:"ua,omitempty"`
	Prefix   string `json:"prefix,omitempty"`
	Filter   string `json:"filter,omitempty"`
	Exclude  string `json:"exclude,omitempty"`
	Sort     bool   `json:"sort,omitempty"`
	Insecure bool   `json:"insecure,omitempty"`
	UDP      bool   `json:"udp,omitempty"`
	TFO      bool   `json:"tfo,omitempty"`
}

// Config is loaded from a JSON file (see config.example.json) and can be
// overridden by flags/environment variables in main.
type Config struct {
	Listen        string `json:"listen"`
	Token         string `json:"token"`
	DefaultUA     string `json:"default_ua"`
	CacheTTL      string `json:"cache_ttl"`
	Subscriptions []Sub  `json:"subscriptions"`
}

// LoadFile reads and normalizes a JSON config.
func LoadFile(path string) (Config, error) {
	var cfg Config
	raw, err := os.ReadFile(path)
	if err != nil {
		return cfg, fmt.Errorf("read config: %w", err)
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return cfg, fmt.Errorf("parse config %s: %w", path, err)
	}
	if err := cfg.Normalize(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// Normalize applies defaults and validates the subscription list.
func (c *Config) Normalize() error {
	c.Listen = strings.TrimSpace(c.Listen)
	if c.Listen == "" {
		c.Listen = defaultListen
	}
	c.DefaultUA = strings.TrimSpace(c.DefaultUA)
	if c.DefaultUA == "" {
		c.DefaultUA = defaultUA
	}
	c.Token = strings.TrimSpace(c.Token)
	c.CacheTTL = strings.TrimSpace(c.CacheTTL)
	if c.CacheTTL == "" {
		c.CacheTTL = defaultTTL.String()
	}
	if _, err := time.ParseDuration(c.CacheTTL); err != nil {
		return fmt.Errorf("invalid cache_ttl %q: %w", c.CacheTTL, err)
	}

	if len(c.Subscriptions) == 0 {
		return fmt.Errorf("config has no subscriptions")
	}

	seen := make(map[string]bool, len(c.Subscriptions))
	for i := range c.Subscriptions {
		sub := &c.Subscriptions[i]
		sub.Name = strings.TrimSpace(sub.Name)
		sub.URL = strings.TrimSpace(sub.URL)
		sub.UA = strings.TrimSpace(sub.UA)

		if sub.Name == "" {
			if len(c.Subscriptions) == 1 {
				sub.Name = "default"
			} else {
				return fmt.Errorf("subscriptions[%d]: name is required when there are multiple subscriptions", i)
			}
		}
		if strings.ContainsAny(sub.Name, "/?#%") || strings.ContainsAny(sub.Name, " \t") {
			return fmt.Errorf("subscriptions[%d]: name %q must not contain spaces, /, ?, # or %%", i, sub.Name)
		}
		if seen[sub.Name] {
			return fmt.Errorf("subscriptions[%d]: duplicate name %q", i, sub.Name)
		}
		seen[sub.Name] = true

		if sub.URL == "" {
			return fmt.Errorf("subscriptions[%d] (%s): url is required", i, sub.Name)
		}
		parsed, err := url.Parse(sub.URL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return fmt.Errorf("subscriptions[%d] (%s): url must be an absolute http(s) URL", i, sub.Name)
		}
	}
	return nil
}

// TTL returns the parsed cache TTL (Normalize guarantees it parses).
func (c Config) TTL() time.Duration {
	ttl, err := time.ParseDuration(c.CacheTTL)
	if err != nil {
		return defaultTTL
	}
	return ttl
}

// DefaultName returns the subscription served at "/" and "/sub".
func (c Config) DefaultName() string {
	for _, sub := range c.Subscriptions {
		if sub.Name == "default" {
			return sub.Name
		}
	}
	return c.Subscriptions[0].Name
}
