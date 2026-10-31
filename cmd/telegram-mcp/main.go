package main

import (
	"fmt"
	"io"
	"os"
)

const usage = `usage: telegram-mcp <command>

commands:
  login    sign in to Telegram and save the session
  serve    run the MCP server on stdio
  logout   delete the saved session

environment:
  TELEGRAM_API_ID         Telegram API id (number, required)
  TELEGRAM_API_HASH       Telegram API hash (required)
  TELEGRAM_SESSION_PATH   session file (default ~/.config/telegram-mcp/session.json)
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	switch args[0] {
	case "serve":
		return runServe(args[1:], stderr)
	case "login":
		return runLogin(stdout, stderr)
	case "logout":
		return runLogout(stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n%s", args[0], usage)
		return 2
	}
}

func runServe(args []string, stderr io.Writer) int {
	fmt.Fprintln(stderr, "not implemented")
	return 2
}

func runLogin(stdout, stderr io.Writer) int {
	fmt.Fprintln(stderr, "not implemented")
	return 2
}

func runLogout(stdout, stderr io.Writer) int {
	fmt.Fprintln(stderr, "not implemented")
	return 2
}
