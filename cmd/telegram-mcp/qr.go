package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/telegram/auth/qrlogin"
	"github.com/gotd/td/tgerr"
	"github.com/mdp/qrterminal/v4"
)

// errInvalidPassword is gotd's wrong-2FA-password error.
var errInvalidPassword = auth.ErrPasswordInvalid

// loginMode parses login flags: --code selects the phone-and-code flow
// instead of the default QR flow.
func loginMode(args []string) (useCode bool, err error) {
	fs := flag.NewFlagSet("login", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.BoolVar(&useCode, "code", false, "log in with a phone number and a code instead of a QR code")
	if err := fs.Parse(args); err != nil {
		return false, fmt.Errorf("login: %w", err)
	}
	return useCode, nil
}

// showQR returns the callback gotd calls for each fresh login token: it draws
// the QR code, prints the URL as text and tells the user what to do.
func showQR(out io.Writer) func(ctx context.Context, token qrlogin.Token) error {
	return func(_ context.Context, token qrlogin.Token) error {
		fmt.Fprintln(out, "\nScan this QR code in Telegram: Settings → Devices → Link Desktop Device")
		qrterminal.GenerateHalfBlock(token.URL(), qrterminal.L, out)
		fmt.Fprintf(out, "Or open this link on a logged-in device: %s\n", token.URL())
		fmt.Fprintln(out, "Waiting for scan (the code refreshes when it expires)...")
		return nil
	}
}

// passwordLoop asks for the 2FA password until check accepts it or fails
// with something other than a wrong password.
func passwordLoop(ctx context.Context, ta termAuth, check func(ctx context.Context, pw string) error) error {
	for {
		pw, err := ta.Password(ctx)
		if err != nil {
			return err
		}
		err = check(ctx, pw)
		if errors.Is(err, errInvalidPassword) {
			fmt.Fprintln(ta.out, "Wrong password, try again.")
			continue
		}
		return err
	}
}

// loginQR runs the QR login flow inside client.Run. The client must have been
// built with the dispatcher that feeds loggedIn, because the scan arrives as an update.
func loginQR(ctx context.Context, client *telegram.Client, stdout io.Writer, ta termAuth, loggedIn qrlogin.LoggedIn) error {
	_, err := client.QR().Auth(ctx, loggedIn, showQR(stdout))
	if err == nil {
		return nil
	}
	if !tgerr.Is(err, "SESSION_PASSWORD_NEEDED") {
		return err
	}
	return passwordLoop(ctx, ta, func(ctx context.Context, pw string) error {
		_, err := client.Auth().Password(ctx, pw)
		return err
	})
}
