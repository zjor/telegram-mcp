// Package config loads telegram-mcp settings from environment variables.
package config

import (
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

// Config holds the settings needed to talk to Telegram.
type Config struct {
	APIID       int
	APIHash     string
	SessionPath string // absolute file path
	Debug       bool   // TELEGRAM_MCP_DEBUG: log every Telegram call to stderr

	DownloadDir      string // absolute; TELEGRAM_MCP_DOWNLOAD_DIR, default <session dir>/downloads
	MaxDownloadBytes int64  // TELEGRAM_MCP_MAX_DOWNLOAD_MB × 1 MiB, default 100 MiB

	SendDirs        []string // absolute, cleaned; TELEGRAM_MCP_SEND_DIRS (os.PathListSeparator-separated), default [DownloadDir]
	SendDirsDefault bool     // true when TELEGRAM_MCP_SEND_DIRS gave no non-empty entries
	MaxUploadBytes  int64    // TELEGRAM_MCP_MAX_UPLOAD_MB × 1 MiB, default 100 MiB
}

const (
	defaultMaxDownloadMB = 100
	defaultMaxUploadMB   = 100
)

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
	dir := getenv("TELEGRAM_MCP_DOWNLOAD_DIR")
	if dir == "" {
		dir = filepath.Join(filepath.Dir(session), "downloads")
	}
	if !filepath.IsAbs(dir) {
		abs, err := filepath.Abs(dir)
		if err != nil {
			return Config{}, fmt.Errorf("TELEGRAM_MCP_DOWNLOAD_DIR: %w", err)
		}
		dir = abs
	}
	maxMB := int64(defaultMaxDownloadMB)
	if v := getenv("TELEGRAM_MCP_MAX_DOWNLOAD_MB"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n <= 0 {
			return Config{}, fmt.Errorf("TELEGRAM_MCP_MAX_DOWNLOAD_MB must be a positive integer, got %q", v)
		}
		maxMB = n
	}
	maxUploadMB := int64(defaultMaxUploadMB)
	if v := getenv("TELEGRAM_MCP_MAX_UPLOAD_MB"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n <= 0 {
			return Config{}, fmt.Errorf("TELEGRAM_MCP_MAX_UPLOAD_MB must be a positive integer, got %q", v)
		}
		maxUploadMB = n
	}
	sendDirs, sendDirsDefault, err := parseSendDirs(getenv("TELEGRAM_MCP_SEND_DIRS"), dir)
	if err != nil {
		return Config{}, err
	}
	return Config{
		APIID:            id,
		APIHash:          hash,
		SessionPath:      session,
		Debug:            debugEnabled(getenv("TELEGRAM_MCP_DEBUG")),
		DownloadDir:      dir,
		MaxDownloadBytes: maxMB << 20,
		SendDirs:         sendDirs,
		SendDirsDefault:  sendDirsDefault,
		MaxUploadBytes:   maxUploadMB << 20,
	}, nil
}

// parseSendDirs splits v on os.PathListSeparator and returns absolute, cleaned
// entries. Empty entries are skipped. When no entry remains, it returns
// [defaultDir] and true.
func parseSendDirs(v, defaultDir string) ([]string, bool, error) {
	var dirs []string
	for _, entry := range filepath.SplitList(v) {
		if entry == "" {
			continue
		}
		abs, err := filepath.Abs(entry)
		if err != nil {
			return nil, false, fmt.Errorf("TELEGRAM_MCP_SEND_DIRS: %w", err)
		}
		dirs = append(dirs, abs)
	}
	if len(dirs) == 0 {
		return []string{defaultDir}, true, nil
	}
	return dirs, false, nil
}

// debugEnabled treats any value except "", "0" and "false" as on.
func debugEnabled(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "0", "false":
		return false
	}
	return true
}
