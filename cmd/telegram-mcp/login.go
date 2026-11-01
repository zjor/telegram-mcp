package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
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

// ask prints prompt and runs read in a goroutine, so a cancelled ctx ends the
// prompt at once. The abandoned read stays blocked on stdin until the process exits.
func (a termAuth) ask(ctx context.Context, prompt string, read func() (string, error)) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	fmt.Fprint(a.out, prompt)
	type result struct {
		s   string
		err error
	}
	ch := make(chan result, 1)
	go func() {
		s, err := read()
		ch <- result{s, err}
	}()
	select {
	case r := <-ch:
		return r.s, r.err
	case <-ctx.Done():
		fmt.Fprintln(a.out)
		return "", ctx.Err()
	}
}

func (a termAuth) readLine() (string, error) {
	line, err := a.in.ReadString('\n')
	if err != nil && !(errors.Is(err, io.EOF) && line != "") {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func (a termAuth) Phone(ctx context.Context) (string, error) {
	return a.ask(ctx, "Phone number (international format): ", a.readLine)
}

// codeHint says where Telegram delivered the login code.
func codeHint(sc *tgapi.AuthSentCode) string {
	if sc == nil {
		return "Code sent."
	}
	switch sc.Type.(type) {
	case *tgapi.AuthSentCodeTypeApp:
		return "Code sent to your other Telegram app (check the \"Telegram\" service chat)."
	case *tgapi.AuthSentCodeTypeSMS, *tgapi.AuthSentCodeTypeSMSWord, *tgapi.AuthSentCodeTypeSMSPhrase, *tgapi.AuthSentCodeTypeFirebaseSMS:
		return "Code sent by SMS."
	case *tgapi.AuthSentCodeTypeCall:
		return "Code will be dictated by a phone call."
	case *tgapi.AuthSentCodeTypeFlashCall, *tgapi.AuthSentCodeTypeMissedCall:
		return "Code is the last digits of the number that calls you."
	case *tgapi.AuthSentCodeTypeEmailCode:
		return "Code sent to your login email."
	case *tgapi.AuthSentCodeTypeFragmentSMS:
		return "Code sent via Fragment SMS."
	default:
		return "Code sent."
	}
}

func (a termAuth) Code(ctx context.Context, sc *tgapi.AuthSentCode) (string, error) {
	fmt.Fprintln(a.out, codeHint(sc))
	return a.ask(ctx, "Code: ", a.readLine)
}

func (a termAuth) Password(ctx context.Context) (string, error) {
	return a.ask(ctx, "2FA password: ", a.readPassword)
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

	// If Ctrl-C ends the password prompt, term.ReadPassword never restores echo.
	if st, err := term.GetState(int(os.Stdin.Fd())); err == nil {
		defer term.Restore(int(os.Stdin.Fd()), st)
	}

	client := newClient(cfg, newLogger(stderr, cfg.Debug))
	loggedIn := false
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
		loggedIn = true
		return nil
	})
	return loginExit(err, loggedIn, stderr)
}

// loginExit maps the result of client.Run to an exit code. gotd's Run returns
// nil when ctx is cancelled, so success needs loggedIn as well as a nil error.
func loginExit(err error, loggedIn bool, stderr io.Writer) int {
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if !loggedIn {
		fmt.Fprintln(stderr, "login interrupted")
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

	client := newClient(cfg, newLogger(stderr, cfg.Debug))
	err = client.Run(ctx, func(ctx context.Context) error {
		_, err := client.API().AuthLogOut(ctx)
		return err
	})
	// gotd's Run returns nil when ctx is cancelled.
	if err == nil && ctx.Err() != nil {
		err = ctx.Err()
	}
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
