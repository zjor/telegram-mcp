package tg

import (
	"encoding/base64"
	"errors"
	"testing"
)

func TestCursor_RoundTrip(t *testing.T) {
	for _, in := range []cursor{
		{Date: 1700000000, ID: 42, PeerType: ChatSupergroup, PeerID: -100123, Folder: 1},
		{Date: 1700000000, ID: 42, PeerType: ChatPrivate, PeerID: 7, Folder: 0},
		{Folder: 1, FromTop: true}, // pinned-only marker: empty PeerType allowed
	} {
		got, err := decodeCursor(encodeCursor(in))
		if err != nil {
			t.Fatalf("decode %+v: %v", in, err)
		}
		if got != in {
			t.Fatalf("got %+v, want %+v", got, in)
		}
	}
}

func TestCursor_Invalid(t *testing.T) {
	enc := base64.RawURLEncoding.EncodeToString
	for _, s := range []string{"not base64!", enc([]byte("{}")), enc([]byte(`{"t":"bogus"}`)), enc([]byte(`{"t":"","x":false}`))} {
		if _, err := decodeCursor(s); !errors.Is(err, ErrInvalidCursor) {
			t.Errorf("decodeCursor(%q) err = %v, want ErrInvalidCursor", s, err)
		}
	}
}
