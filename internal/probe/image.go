// Package probe reads media file headers. It uses only file content, never
// the file extension, and treats all input as untrusted.
package probe

import (
	"bytes"
	"errors"
	"io"
)

// ErrUnknownFormat means the data is not in a format the reader handles.
var ErrUnknownFormat = errors.New("unknown format")

// ImageFormat returns "jpeg", "png" or "webp", or ErrUnknownFormat.
func ImageFormat(r io.ReaderAt) (string, error) {
	var b [12]byte
	n, err := r.ReadAt(b[:], 0)
	if err != nil && err != io.EOF {
		return "", err
	}
	h := b[:n]
	switch {
	case bytes.HasPrefix(h, []byte{0xff, 0xd8, 0xff}):
		return "jpeg", nil
	case bytes.HasPrefix(h, []byte("\x89PNG\r\n\x1a\n")):
		return "png", nil
	case len(h) == 12 && string(h[:4]) == "RIFF" && string(h[8:12]) == "WEBP":
		return "webp", nil
	}
	return "", ErrUnknownFormat
}
