// Package config loads telegram-mcp settings from environment variables.
package config

import (
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
)

// Config holds the settings needed to talk to Telegram.
type Config struct {
	APIID       int
	APIHash     string
	SessionPath string // absolute file path
}

// PeersPath returns the peer cache path, next to the session file.
func (c Config) PeersPath() string {
	return filepath.Join(filepath.Dir(c.SessionPath), "peers.json")
}

// Load reads the config using getenv. homeDir is used for the default session path.
func Load(getenv func(string) string, homeDir string) (Config, error) {
	idStr := getenv("TELEGRAM_API_ID")
	if idStr == "" {
		return Config{}, errors.New("TELEGRAM_API_ID is not set")
	}
	id, err := strconv.Atoi(idStr)
	if err != nil {
		return Config{}, fmt.Errorf("TELEGRAM_API_ID must be a number, got %q", idStr)
	}
	hash := getenv("TELEGRAM_API_HASH")
	if hash == "" {
		return Config{}, errors.New("TELEGRAM_API_HASH is not set")
	}
	session := getenv("TELEGRAM_SESSION_PATH")
	if session == "" {
		session = filepath.Join(homeDir, ".config", "telegram-mcp", "session.json")
	}
	if !filepath.IsAbs(session) {
		abs, err := filepath.Abs(session)
		if err != nil {
			return Config{}, fmt.Errorf("TELEGRAM_SESSION_PATH: %w", err)
		}
		session = abs
	}
	return Config{APIID: id, APIHash: hash, SessionPath: session}, nil
}
