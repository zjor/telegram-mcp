package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/gotd/td/telegram/auth"
	tgapi "github.com/gotd/td/tg"
	"golang.org/x/term"

	"github.com/zjor/telegram-mcp/internal/config"
	"github.com/zjor/telegram-mcp/internal/tg"
)

// termAuth answers gotd's auth prompts from a terminal.
type termAuth struct {
	in           *bufio.Reader
	out          io.Writer
	readPassword func() (string, error)
}

var _ auth.UserAuthenticator = termAuth{}

func (a termAuth) readLine(prompt string) (string, error) {
	fmt.Fprint(a.out, prompt)
	line, err := a.in.ReadString('\n')
	if err != nil && !(errors.Is(err, io.EOF) && line != "") {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func (a termAuth) Phone(ctx context.Context) (string, error) {
	return a.readLine("Phone number (international format): ")
}

func (a termAuth) Code(ctx context.Context, _ *tgapi.AuthSentCode) (string, error) {
	return a.readLine("Code: ")
}

func (a termAuth) Password(ctx context.Context) (string, error) {
	fmt.Fprint(a.out, "2FA password: ")
	return a.readPassword()
}

func (a termAuth) AcceptTermsOfService(ctx context.Context, _ tgapi.HelpTermsOfService) error {
	return nil
}

func (a termAuth) SignUp(ctx context.Context) (auth.UserInfo, error) {
	return auth.UserInfo{}, errors.New("sign up is not supported; create the account in a Telegram app first")
}

// stdinPassword reads a password from the terminal without echo.
func stdinPassword(out io.Writer) func() (string, error) {
	return func() (string, error) {
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(out)
		return string(b), err
	}
}

// loadConfig loads the config from the process environment.
func loadConfig() (config.Config, error) {
	home, _ := os.UserHomeDir()
	return config.Load(os.Getenv, home)
}

func runLogin(stdout, stderr io.Writer) int {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	client := newClient(cfg, slog.New(slog.NewTextHandler(stderr, nil)))
	ta := termAuth{in: bufio.NewReader(os.Stdin), out: stdout, readPassword: stdinPassword(stdout)}
	err = client.Run(ctx, func(ctx context.Context) error {
		if err := client.Auth().IfNecessary(ctx, auth.NewFlow(ta, auth.SendCodeOptions{})); err != nil {
			return err
		}
		self, err := client.Self(ctx)
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "Logged in as %s\n", tg.UserName(self))
		return nil
	})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func runLogout(stdout, stderr io.Writer) int {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if _, err := os.Stat(cfg.SessionPath); errors.Is(err, os.ErrNotExist) {
		fmt.Fprintln(stdout, "no session")
		return 0
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	client := newClient(cfg, slog.New(slog.NewTextHandler(stderr, nil)))
	err = client.Run(ctx, func(ctx context.Context) error {
		_, err := client.API().AuthLogOut(ctx)
		return err
	})
	if err != nil {
		fmt.Fprintf(stderr, "server logout failed: %v; removing local files anyway\n", err)
	}
	for _, p := range []string{cfg.SessionPath, cfg.PeersPath()} {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			fmt.Fprintln(stderr, err)
		}
	}
	fmt.Fprintln(stdout, "Logged out")
	return 0
}
