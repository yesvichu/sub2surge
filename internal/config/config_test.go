package config

import (
	"strings"
	"testing"
	"time"
)

func TestNormalizeDefaults(t *testing.T) {
	cfg := Config{Subscriptions: []Sub{{URL: "https://example.com/sub"}}}
	if err := cfg.Normalize(); err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if cfg.Listen != defaultListen || cfg.DefaultUA != defaultUA || cfg.TTL() != time.Hour {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	if cfg.Subscriptions[0].Name != "default" || cfg.DefaultName() != "default" {
		t.Fatalf("unexpected name defaulting: %+v", cfg.Subscriptions[0])
	}
}

func TestNormalizeValidation(t *testing.T) {
	cases := []Config{
		{},
		{Subscriptions: []Sub{{Name: "a", URL: "not-a-url"}}},
		{Subscriptions: []Sub{{Name: "a", URL: "https://a"}, {Name: "a", URL: "https://b"}}},
		{Subscriptions: []Sub{{Name: "a/b", URL: "https://a"}}},
		{CacheTTL: "soon", Subscriptions: []Sub{{URL: "https://a"}}},
	}
	for _, cfg := range cases {
		if err := cfg.Normalize(); err == nil {
			t.Fatalf("expected error for %+v", cfg)
		}
	}
}

func TestNormalizeMultipleRequiresNames(t *testing.T) {
	cfg := Config{Subscriptions: []Sub{
		{URL: "https://a"},
		{URL: "https://b"},
	}}
	err := cfg.Normalize()
	if err == nil || !strings.Contains(err.Error(), "name is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}
