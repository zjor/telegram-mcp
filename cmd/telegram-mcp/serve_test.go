package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeInfo struct{}

func (fakeInfo) Name() string       { return "session.json" }
func (fakeInfo) Size() int64        { return 1 }
func (fakeInfo) Mode() os.FileMode  { return 0o600 }
func (fakeInfo) ModTime() time.Time { return time.Time{} }
func (fakeInfo) IsDir() bool        { return false }
func (fakeInfo) Sys() any           { return nil }

var testEnv = map[string]string{
	"TELEGRAM_API_ID":       "1",
	"TELEGRAM_API_HASH":     "x",
	"TELEGRAM_SESSION_PATH": "/data/tg/session.json",
}

func getenv(k string) string { return testEnv[k] }

func statOK(string) (os.FileInfo, error)      { return fakeInfo{}, nil }
func statMissing(string) (os.FileInfo, error) { return nil, os.ErrNotExist }

func TestServeConfig_BadTransport(t *testing.T) {
	_, _, err := serveConfig([]string{"--transport", "http"}, getenv, "/home/u", statOK)
	if err == nil || !strings.Contains(err.Error(), "only stdio") {
		t.Fatalf("err = %v", err)
	}
}

func TestServeConfig_MissingSession(t *testing.T) {
	_, _, err := serveConfig(nil, getenv, "/home/u", statMissing)
	if err == nil {
		t.Fatal("want error")
	}
	for _, w := range []string{`run "telegram-mcp login"`, "/data/tg/session.json"} {
		if !strings.Contains(err.Error(), w) {
			t.Errorf("err %q missing %q", err, w)
		}
	}
}

func TestServeConfig_OK(t *testing.T) {
	cfg, transport, err := serveConfig(nil, getenv, "/home/u", statOK)
	if err != nil {
		t.Fatal(err)
	}
	if transport != "stdio" {
		t.Errorf("transport = %q", transport)
	}
	if cfg.APIID != 1 || cfg.APIHash != "x" || cfg.SessionPath != "/data/tg/session.json" {
		t.Errorf("cfg = %+v", cfg)
	}
}

func TestRunServe_ExitCodes(t *testing.T) {
	t.Setenv("TELEGRAM_API_ID", "1")
	t.Setenv("TELEGRAM_API_HASH", "x")
	t.Setenv("TELEGRAM_SESSION_PATH", filepath.Join(t.TempDir(), "none", "session.json"))

	var errb bytes.Buffer
	if code := runServe([]string{"--transport", "http"}, &errb); code != 2 {
		t.Errorf("bad transport: code = %d, want 2", code)
	}
	if !strings.Contains(errb.String(), "only stdio") {
		t.Errorf("bad transport stderr: %q", errb.String())
	}

	errb.Reset()
	if code := runServe(nil, &errb); code != 3 {
		t.Errorf("missing session: code = %d, want 3", code)
	}
	if !strings.Contains(errb.String(), `run "telegram-mcp login"`) {
		t.Errorf("missing session stderr: %q", errb.String())
	}

	errb.Reset()
	t.Setenv("TELEGRAM_API_ID", "")
	if code := runServe(nil, &errb); code != 2 {
		t.Errorf("bad config: code = %d, want 2", code)
	}
}
