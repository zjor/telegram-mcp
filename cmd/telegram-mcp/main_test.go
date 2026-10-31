package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRun_Dispatch(t *testing.T) {
	for _, args := range [][]string{nil, {"bogus"}, {"serve", "--transport", "http"}, {"login"}, {"logout"}} {
		var out, errb bytes.Buffer
		if code := run(args, &out, &errb); code != 2 {
			t.Errorf("run(%v) = %d, want 2", args, code)
		}
		if errb.Len() == 0 {
			t.Errorf("run(%v): nothing on stderr", args)
		}
		if out.Len() != 0 {
			t.Errorf("run(%v): stdout not empty", args)
		}
	}
	var errb bytes.Buffer
	run(nil, &bytes.Buffer{}, &errb)
	for _, w := range []string{"login", "serve", "logout", "TELEGRAM_API_ID", "TELEGRAM_API_HASH", "TELEGRAM_SESSION_PATH"} {
		if !strings.Contains(errb.String(), w) {
			t.Errorf("usage missing %q", w)
		}
	}
}
