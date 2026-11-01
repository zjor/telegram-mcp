package tg

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/gotd/contrib/middleware/floodwait"
	"github.com/gotd/log/logslog"
	"github.com/gotd/td/bin"
	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram"
	tgapi "github.com/gotd/td/tg"
)

// ClientConfig holds what NewClient needs to build a gotd client.
type ClientConfig struct {
	APIID       int
	APIHash     string
	SessionPath string
	Logger      *slog.Logger // our own diagnostics
	Debug       bool         // also log every RPC and gotd's own events through Logger
}

// NewClient builds a gotd client with a file session, flood-wait handling and
// no update processing. The session directory is created with mode 0700 first.
// Without Debug, gotd's own logger stays off, so nothing from gotd reaches stdout.
func NewClient(cfg ClientConfig) *telegram.Client {
	if err := os.MkdirAll(filepath.Dir(cfg.SessionPath), 0o700); err != nil && cfg.Logger != nil {
		cfg.Logger.Warn("cannot create session directory", "error", err)
	}
	opts := telegram.Options{
		SessionStorage: &session.FileStorage{Path: cfg.SessionPath},
		Middlewares: []telegram.Middleware{
			floodwait.NewSimpleWaiter().WithMaxWait(60 * time.Second),
		},
		NoUpdates: true,
	}
	if cfg.Debug && cfg.Logger != nil {
		opts.Middlewares = append(opts.Middlewares, RPCLogger(cfg.Logger))
		opts.Logger = logslog.New(cfg.Logger.With("component", "gotd"))
	}
	return telegram.NewClient(cfg.APIID, cfg.APIHash, opts)
}

// RPCLogger returns a middleware that logs each MTProto call at debug level:
// the request type, the response type, the duration and the error, if any.
func RPCLogger(logger *slog.Logger) telegram.Middleware {
	return telegram.MiddlewareFunc(func(next tgapi.Invoker) telegram.InvokeFunc {
		return func(ctx context.Context, input bin.Encoder, output bin.Decoder) error {
			start := time.Now()
			err := next.Invoke(ctx, input, output)
			attrs := []any{
				"method", typeName(input),
				"duration", time.Since(start).Round(time.Millisecond),
			}
			if err != nil {
				logger.Debug("rpc failed", append(attrs, "error", err)...)
				return err
			}
			logger.Debug("rpc ok", append(attrs, "result", typeName(output))...)
			return nil
		}
	})
}

// typeName returns the TL type name for gotd request/response objects.
func typeName(v any) string {
	if n, ok := v.(interface{ TypeName() string }); ok {
		return n.TypeName()
	}
	if b, ok := v.(interface{ Elem() bin.Decoder }); ok {
		// Boxed results (e.g. *tg.AuthSentCodeBox) name their inner type only after decoding.
		return typeName(b.Elem())
	}
	return fmt.Sprintf("%T", v)
}
