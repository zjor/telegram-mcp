package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	tgapi "github.com/gotd/td/tg"
)

func TestTermAuth_PhoneAndCode(t *testing.T) {
	var out bytes.Buffer
	a := termAuth{in: bufio.NewReader(strings.NewReader("  +420123 \n12345\n")), out: &out}
	ctx := context.Background()
	phone, err := a.Phone(ctx)
	if err != nil || phone != "+420123" {
		t.Fatalf("Phone() = %q, %v; want +420123", phone, err)
	}
	code, err := a.Code(ctx, &tgapi.AuthSentCode{})
	if err != nil || code != "12345" {
		t.Fatalf("Code() = %q, %v; want 12345", code, err)
	}
	for _, p := range []string{"Phone number (international format): ", "Code: "} {
		if !strings.Contains(out.String(), p) {
			t.Errorf("output %q missing prompt %q", out.String(), p)
		}
	}
}

func TestTermAuth_Password(t *testing.T) {
	var out bytes.Buffer
	a := termAuth{out: &out, readPassword: func() (string, error) { return "s3cret", nil }}
	pw, err := a.Password(context.Background())
	if err != nil || pw != "s3cret" {
		t.Fatalf("Password() = %q, %v; want s3cret", pw, err)
	}
	if !strings.Contains(out.String(), "2FA password: ") {
		t.Errorf("output %q missing password prompt", out.String())
	}
}

func TestTermAuth_SignUpRefused(t *testing.T) {
	_, err := termAuth{}.SignUp(context.Background())
	if err == nil || err.Error() != "sign up is not supported; create the account in a Telegram app first" {
		t.Fatalf("SignUp() error = %v", err)
	}
}

func TestRunLogout_NoSession(t *testing.T) {
	t.Setenv("TELEGRAM_API_ID", "1")
	t.Setenv("TELEGRAM_API_HASH", "h")
	t.Setenv("TELEGRAM_SESSION_PATH", t.TempDir()+"/missing.json")
	var out, errb bytes.Buffer
	if code := runLogout(&out, &errb); code != 0 {
		t.Fatalf("runLogout = %d, stderr %q", code, errb.String())
	}
	if out.String() != "no session\n" || errb.Len() != 0 {
		t.Errorf("stdout %q, stderr %q", out.String(), errb.String())
	}
}

func TestTermAuth_CancelledCtxStopsBlockedRead(t *testing.T) {
	pr, pw := io.Pipe() // nothing is written, so a read blocks
	defer pw.Close()
	block := make(chan struct{})
	defer close(block)
	a := termAuth{
		in:           bufio.NewReader(pr),
		out:          io.Discard,
		readPassword: func() (string, error) { <-block; return "", nil },
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := map[string]func() (string, error){
		"Phone":    func() (string, error) { return a.Phone(ctx) },
		"Password": func() (string, error) { return a.Password(ctx) },
	}
	for name, call := range calls {
		done := make(chan error, 1)
		go func() { _, err := call(); done <- err }()
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Errorf("%s error = %v, want context.Canceled", name, err)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("%s did not return after ctx was cancelled", name)
		}
	}
}

func TestTermAuth_CancelDuringRead(t *testing.T) {
	pr, pw := io.Pipe()
	defer pw.Close()
	a := termAuth{in: bufio.NewReader(pr), out: io.Discard}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := a.Phone(ctx); done <- err }()
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Phone error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Phone did not return after cancel")
	}
}

func TestLoginExit(t *testing.T) {
	cases := []struct {
		err      error
		loggedIn bool
		code     int
		stderr   string
	}{
		{nil, true, 0, ""},
		{nil, false, 1, "login interrupted\n"},
		{errors.New("boom"), false, 1, "boom\n"},
	}
	for _, c := range cases {
		var errb bytes.Buffer
		if got := loginExit(c.err, c.loggedIn, &errb); got != c.code || errb.String() != c.stderr {
			t.Errorf("loginExit(%v, %v) = %d, %q; want %d, %q", c.err, c.loggedIn, got, errb.String(), c.code, c.stderr)
		}
	}
}
