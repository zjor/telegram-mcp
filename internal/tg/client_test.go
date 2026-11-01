package tg

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/gotd/td/bin"
	tgapi "github.com/gotd/td/tg"
)

type fakeInvoker struct {
	err error
}

func (f fakeInvoker) Invoke(_ context.Context, _ bin.Encoder, _ bin.Decoder) error {
	return f.err
}

func TestRPCLogger_LogsMethodAndResult(t *testing.T) {
	var out bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&out, &slog.HandlerOptions{Level: slog.LevelDebug}))
	next := RPCLogger(logger).Handle(fakeInvoker{})

	var res tgapi.AuthSentCode
	if err := next.Invoke(context.Background(), &tgapi.AuthSendCodeRequest{PhoneNumber: "+1"}, &res); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{"rpc ok", "method=auth.sendCode", "result=auth.sentCode", "duration="} {
		if !strings.Contains(got, want) {
			t.Errorf("log %q missing %q", got, want)
		}
	}
	if strings.Contains(got, "+1") {
		t.Errorf("log %q leaks request payload", got)
	}
}

func TestRPCLogger_LogsError(t *testing.T) {
	var out bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&out, &slog.HandlerOptions{Level: slog.LevelDebug}))
	boom := errors.New("PHONE_NUMBER_INVALID")
	next := RPCLogger(logger).Handle(fakeInvoker{err: boom})

	err := next.Invoke(context.Background(), &tgapi.AuthSendCodeRequest{}, &tgapi.AuthSentCode{})
	if !errors.Is(err, boom) {
		t.Fatalf("Invoke() error = %v; want %v", err, boom)
	}
	got := out.String()
	for _, want := range []string{"rpc failed", "method=auth.sendCode", "error=PHONE_NUMBER_INVALID"} {
		if !strings.Contains(got, want) {
			t.Errorf("log %q missing %q", got, want)
		}
	}
}

func TestRPCLogger_SilentAtInfoLevel(t *testing.T) {
	var out bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&out, nil))
	next := RPCLogger(logger).Handle(fakeInvoker{})
	if err := next.Invoke(context.Background(), &tgapi.AuthSendCodeRequest{}, &tgapi.AuthSentCode{}); err != nil {
		t.Fatal(err)
	}
	if out.Len() != 0 {
		t.Errorf("expected no output at info level, got %q", out.String())
	}
}
