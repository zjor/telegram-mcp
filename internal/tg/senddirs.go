package tg

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

var (
	ErrOutsideSendDirs = errors.New("path is outside the allowed folders (TELEGRAM_MCP_SEND_DIRS)")
	ErrNotRegular      = errors.New("path is not a regular file")
)

// SendDirs is the set of folders send_file may read from. It is the security
// boundary of send_file: the model reads untrusted chat text.
type SendDirs struct{ dirs []string }

// NewSendDirs resolves each dir (symlinks included). A dir that cannot be
// resolved is reported to warn (may be nil) and skipped.
func NewSendDirs(dirs []string, warn func(dir string, err error)) SendDirs {
	var out SendDirs
	for _, dir := range dirs {
		r, err := filepath.EvalSymlinks(dir)
		if err == nil {
			r, err = filepath.Abs(r)
		}
		if err != nil {
			if warn != nil {
				warn(dir, err)
			}
			continue
		}
		out.dirs = append(out.dirs, r)
	}
	return out
}

func (d SendDirs) contains(resolved string) bool {
	for _, dir := range d.dirs {
		rel, err := filepath.Rel(dir, resolved)
		if err != nil {
			continue
		}
		if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		return true
	}
	return false
}

// Open resolves path, checks it is inside an allowed folder and is a regular
// file, and returns it open. Callers must use the returned handle for every
// later step so the file cannot be swapped after the check. A missing file
// returns ErrOutsideSendDirs, the same as an outside path.
func (d SendDirs) Open(path string) (*os.File, string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, "", ErrOutsideSendDirs
	}
	if resolved, err = filepath.Abs(resolved); err != nil || !d.contains(resolved) {
		return nil, "", ErrOutsideSendDirs
	}
	// Check the mode before opening so a FIFO or device is never opened.
	li, err := os.Lstat(resolved)
	if err != nil {
		return nil, "", ErrOutsideSendDirs
	}
	if !li.Mode().IsRegular() {
		return nil, "", ErrNotRegular
	}
	f, err := os.OpenFile(resolved, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, "", ErrOutsideSendDirs
	}
	// The open handle is the authority: it must be the same regular file.
	fi, err := f.Stat()
	if err != nil || !fi.Mode().IsRegular() || !os.SameFile(li, fi) {
		f.Close()
		if err == nil && !fi.Mode().IsRegular() {
			return nil, "", ErrNotRegular
		}
		return nil, "", ErrOutsideSendDirs
	}
	return f, resolved, nil
}
