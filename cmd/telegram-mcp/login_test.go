package main

import (
	"bufio"
	"bytes"
	"context"
	"strings"
	"testing"

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
