package tg

import (
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/gotd/contrib/middleware/floodwait"
	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram"
)

// ClientConfig holds what NewClient needs to build a gotd client.
type ClientConfig struct {
	APIID       int
	APIHash     string
	SessionPath string
	Logger      *slog.Logger // our own diagnostics; gotd's logger stays off
}

// NewClient builds a gotd client with a file session, flood-wait handling and
// no update processing. The session directory is created with mode 0700 first.
// gotd's own logger is not set, so nothing from gotd is written to stdout.
func NewClient(cfg ClientConfig) *telegram.Client {
	if err := os.MkdirAll(filepath.Dir(cfg.SessionPath), 0o700); err != nil && cfg.Logger != nil {
		cfg.Logger.Warn("cannot create session directory", "error", err)
	}
	return telegram.NewClient(cfg.APIID, cfg.APIHash, telegram.Options{
		SessionStorage: &session.FileStorage{Path: cfg.SessionPath},
		Middlewares: []telegram.Middleware{
			floodwait.NewSimpleWaiter().WithMaxWait(60 * time.Second),
		},
		NoUpdates: true,
	})
}
