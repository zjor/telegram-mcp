package config_test

import (
	"path/filepath"
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

func TestLoad_RelativeSessionPathMadeAbsolute(t *testing.T) {
	rel := filepath.Join("data", "s.json")
	cfg, err := load(map[string]string{"TELEGRAM_API_ID": "1", "TELEGRAM_API_HASH": "abc", "TELEGRAM_SESSION_PATH": rel})
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.Abs(rel)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SessionPath != want {
		t.Fatalf("SessionPath = %q, want %q", cfg.SessionPath, want)
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

func TestLoad_Debug(t *testing.T) {
	for v, want := range map[string]bool{"": false, "0": false, "false": false, "1": true, "true": true, "yes": true} {
		env := map[string]string{"TELEGRAM_API_ID": "1", "TELEGRAM_API_HASH": "h", "TELEGRAM_MCP_DEBUG": v}
		cfg, err := config.Load(func(k string) string { return env[k] }, "/home/u")
		if err != nil {
			t.Fatalf("TELEGRAM_MCP_DEBUG=%q: %v", v, err)
		}
		if cfg.Debug != want {
			t.Errorf("TELEGRAM_MCP_DEBUG=%q: Debug = %v; want %v", v, cfg.Debug, want)
		}
	}
}

func TestLoad_DownloadDefaults(t *testing.T) {
	cfg, err := load(map[string]string{"TELEGRAM_API_ID": "1", "TELEGRAM_API_HASH": "abc"})
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(filepath.Dir(cfg.SessionPath), "downloads"); cfg.DownloadDir != want {
		t.Fatalf("DownloadDir = %q, want %q", cfg.DownloadDir, want)
	}
	if cfg.MaxDownloadBytes != 100<<20 {
		t.Fatalf("MaxDownloadBytes = %d, want %d", cfg.MaxDownloadBytes, 100<<20)
	}
}

func TestLoad_DownloadDirFromEnv(t *testing.T) {
	env := map[string]string{"TELEGRAM_API_ID": "1", "TELEGRAM_API_HASH": "abc", "TELEGRAM_MCP_DOWNLOAD_DIR": "/x/dl"}
	cfg, err := load(env)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DownloadDir != "/x/dl" {
		t.Fatalf("DownloadDir = %q, want /x/dl", cfg.DownloadDir)
	}
	rel := filepath.Join("rel", "dl")
	env["TELEGRAM_MCP_DOWNLOAD_DIR"] = rel
	if cfg, err = load(env); err != nil {
		t.Fatal(err)
	}
	want, err := filepath.Abs(rel)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DownloadDir != want {
		t.Fatalf("DownloadDir = %q, want %q", cfg.DownloadDir, want)
	}
}

func TestLoad_MaxDownloadMB(t *testing.T) {
	env := func(v string) map[string]string {
		return map[string]string{"TELEGRAM_API_ID": "1", "TELEGRAM_API_HASH": "abc", "TELEGRAM_MCP_MAX_DOWNLOAD_MB": v}
	}
	cfg, err := load(env("5"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MaxDownloadBytes != 5<<20 {
		t.Fatalf("MaxDownloadBytes = %d, want %d", cfg.MaxDownloadBytes, 5<<20)
	}
	for _, v := range []string{"abc", "0", "-1"} {
		_, err := load(env(v))
		if err == nil || !strings.Contains(err.Error(), "TELEGRAM_MCP_MAX_DOWNLOAD_MB") {
			t.Errorf("value %q: err = %v, want error naming TELEGRAM_MCP_MAX_DOWNLOAD_MB", v, err)
		}
	}
}
