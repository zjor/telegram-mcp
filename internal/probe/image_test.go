package probe

import (
	"bytes"
	"errors"
	"testing"
)

func TestImageFormat(t *testing.T) {
	tests := []struct {
		name string
		data string
		want string
		err  error
	}{
		{"jpeg", "\xff\xd8\xff\xe0\x00\x10JFIF", "jpeg", nil},
		{"png", "\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR", "png", nil},
		{"webp", "RIFF\x00\x00\x00\x00WEBPVP8 ", "webp", nil},
		{"gif", "GIF89a", "", ErrUnknownFormat},
		{"text", "hello", "", ErrUnknownFormat},
		{"empty", "", "", ErrUnknownFormat},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ImageFormat(bytes.NewReader([]byte(tt.data)))
			if !errors.Is(err, tt.err) || got != tt.want {
				t.Errorf("got (%q, %v), want (%q, %v)", got, err, tt.want, tt.err)
			}
		})
	}
}
