package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/gotd/td/telegram"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zjor/telegram-mcp/internal/config"
	"github.com/zjor/telegram-mcp/internal/mcpserver"
	"github.com/zjor/telegram-mcp/internal/tg"
)

// exitError carries the process exit code for a serve setup failure.
type exitError struct {
	code int
	msg  string
}

func (e *exitError) Error() string { return e.msg }

func usageErr(format string, a ...any) error {
	return &exitError{code: 2, msg: fmt.Sprintf(format, a...)}
}

// serveConfig parses serve flags, loads config and checks that a session file
// exists. It does no network I/O. Errors are *exitError: code 2 for usage and
// config problems, code 3 for a missing session.
func serveConfig(args []string, getenv func(string) string, home string, stat func(string) (os.FileInfo, error)) (config.Config, string, error) {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	transport := fs.String("transport", "stdio", "MCP transport (only stdio)")
	if err := fs.Parse(args); err != nil {
		return config.Config{}, "", usageErr("%v", err)
	}
	if *transport != "stdio" {
		return config.Config{}, "", usageErr("unsupported transport %q; only stdio is implemented", *transport)
	}
	cfg, err := config.Load(getenv, home)
	if err != nil {
		return config.Config{}, "", usageErr("%v", err)
	}
	if _, err := stat(cfg.SessionPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return config.Config{}, "", &exitError{code: 3, msg: fmt.Sprintf("no session at %s; run \"telegram-mcp login\" first", cfg.SessionPath)}
		}
		return config.Config{}, "", usageErr("%v", err)
	}
	return cfg, *transport, nil
}

func runServe(args []string, stderr io.Writer) int {
	home, _ := os.UserHomeDir()
	cfg, _, err := serveConfig(args, os.Getenv, home, os.Stat)
	if err != nil {
		fmt.Fprintln(stderr, err)
		var ee *exitError
		if errors.As(err, &ee) {
			return ee.code
		}
		return 2
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger := newLogger(stderr, cfg.Debug)
	if err := serve(ctx, cfg, logger); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func serve(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	client := newClient(cfg, logger, nil)
	return client.Run(ctx, func(ctx context.Context) error {
		status, err := client.Auth().Status(ctx)
		if err != nil {
			return fmt.Errorf("auth status: %w", err)
		}
		if !status.Authorized {
			return errors.New("session is not authorized; run telegram-mcp login")
		}
		peers := tg.NewPeerCache(cfg.PeersPath())
		if err := peers.Load(); err != nil {
			logger.Warn("peer cache not loaded; starting empty", "path", cfg.PeersPath(), "error", err)
			peers = tg.NewPeerCache(cfg.PeersPath())
		}
		// *telegram.Client implements tgapi.Invoker; client.API() does not.
		svc := tg.NewService(client, peers)
		svc.SetDownloads(cfg.DownloadDir, cfg.MaxDownloadBytes)
		if cfg.SendDirsDefault {
			// The default send folder is the download folder. Create it so
			// the path check can resolve it on a fresh install.
			if err := os.MkdirAll(cfg.DownloadDir, 0o700); err != nil {
				logger.Warn("cannot create download folder", "dir", cfg.DownloadDir, "error", err)
			}
		}
		svc.SetUploads(tg.NewSendDirs(cfg.SendDirs, func(dir string, err error) {
			logger.Warn("send dir skipped", "dir", dir, "error", err)
		}), cfg.MaxUploadBytes)
		return mcpserver.New(svc).Run(ctx, &mcp.StdioTransport{})
	})
}

// newClient builds the gotd client shared by serve, login and logout.
func newClient(cfg config.Config, logger *slog.Logger, updates telegram.UpdateHandler) *telegram.Client {
	return tg.NewClient(tg.ClientConfig{
		APIID:       cfg.APIID,
		APIHash:     cfg.APIHash,
		SessionPath: cfg.SessionPath,
		Logger:      logger,
		Debug:       cfg.Debug,
		Updates:     updates,
	})
}

// newLogger writes to stderr; TELEGRAM_MCP_DEBUG lowers the level to debug.
func newLogger(stderr io.Writer, debug bool) *slog.Logger {
	level := slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	}
	return slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: level}))
}
