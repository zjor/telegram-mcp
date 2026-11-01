package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/gotd/td/telegram/auth/qrlogin"
)

func TestLoginMode(t *testing.T) {
	cases := []struct {
		args    []string
		useCode bool
		wantErr bool
	}{
		{nil, false, false},
		{[]string{"--code"}, true, false},
		{[]string{"--bogus"}, false, true},
	}
	for _, c := range cases {
		useCode, err := loginMode(c.args)
		if (err != nil) != c.wantErr || useCode != c.useCode {
			t.Errorf("loginMode(%v) = %v, %v; want %v, err=%v", c.args, useCode, err, c.useCode, c.wantErr)
		}
	}
}

func TestShowQR_PrintsURLAndInstructions(t *testing.T) {
	var out bytes.Buffer
	token := qrlogin.NewToken([]byte("abc"), int(time.Now().Add(30*time.Second).Unix()))
	if err := showQR(&out)(context.Background(), token); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{"Settings", "Link Desktop Device", token.URL(), "Waiting for scan"} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
	if !strings.Contains(got, "█") && !strings.Contains(got, "▄") {
		t.Errorf("output has no QR blocks:\n%s", got)
	}
}

func TestPasswordLoop_RetriesOnInvalidPassword(t *testing.T) {
	var out bytes.Buffer
	pws := []string{"wrong", "right"}
	a := termAuth{out: &out, readPassword: func() (string, error) {
		pw := pws[0]
		pws = pws[1:]
		return pw, nil
	}}
	calls := 0
	check := func(_ context.Context, pw string) error {
		calls++
		if pw != "right" {
			return errInvalidPassword
		}
		return nil
	}
	if err := passwordLoop(context.Background(), a, check); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Errorf("check called %d times; want 2", calls)
	}
	if !strings.Contains(out.String(), "Wrong password, try again.") {
		t.Errorf("output %q missing retry notice", out.String())
	}
}

func TestPasswordLoop_OtherErrorStops(t *testing.T) {
	a := termAuth{out: &bytes.Buffer{}, readPassword: func() (string, error) { return "x", nil }}
	boom := errors.New("network down")
	err := passwordLoop(context.Background(), a, func(context.Context, string) error { return boom })
	if !errors.Is(err, boom) {
		t.Fatalf("passwordLoop error = %v; want %v", err, boom)
	}
}
