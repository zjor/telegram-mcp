package config_test

import (
	"strings"
	"testing"

	"github.com/zjor/telegram-mcp/internal/config"
)

func load(env map[string]string) (config.Config, error) {
	return config.Load(func(k string) string { return env[k] }, "/home/u")
}

func TestLoad_Defaults(t *testing.T) {
	cfg, err := load(map[string]string{"TELEGRAM_API_ID": "12345", "TELEGRAM_API_HASH": "abc"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.APIID != 12345 || cfg.APIHash != "abc" {
		t.Fatalf("got %+v", cfg)
	}
	if cfg.SessionPath != "/home/u/.config/telegram-mcp/session.json" {
		t.Fatalf("session path %q", cfg.SessionPath)
	}
	if cfg.PeersPath() != "/home/u/.config/telegram-mcp/peers.json" {
		t.Fatalf("peers path %q", cfg.PeersPath())
	}
}

func TestLoad_SessionPathOverride(t *testing.T) {
	cfg, err := load(map[string]string{"TELEGRAM_API_ID": "1", "TELEGRAM_API_HASH": "abc", "TELEGRAM_SESSION_PATH": "/tmp/s.json"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SessionPath != "/tmp/s.json" || cfg.PeersPath() != "/tmp/peers.json" {
		t.Fatalf("got %q %q", cfg.SessionPath, cfg.PeersPath())
	}
}

func TestLoad_Errors(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"MissingAPIID", map[string]string{"TELEGRAM_API_HASH": "abc"}, "TELEGRAM_API_ID"},
		{"NonNumericAPIID", map[string]string{"TELEGRAM_API_ID": "abc", "TELEGRAM_API_HASH": "abc"}, "TELEGRAM_API_ID"},
		{"MissingAPIHash", map[string]string{"TELEGRAM_API_ID": "1"}, "TELEGRAM_API_HASH"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := load(tt.env)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want mention of %s", err, tt.want)
			}
		})
	}
}
