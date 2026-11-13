package tg

import (
	"encoding/base64"
	"errors"
	"testing"
)

func TestParticipantsCursor_RoundTrip(t *testing.T) {
	for _, in := range []participantsCursor{
		{ChatID: 42, Source: SourceRecentSenders, BeforeID: 900},
		{ChatID: 42, Source: SourceMembers, Offset: 200},
	} {
		out, err := decodeParticipantsCursor(encodeParticipantsCursor(in), 42)
		if err != nil {
			t.Fatalf("decode %+v: %v", in, err)
		}
		if out != in {
			t.Fatalf("got %+v, want %+v", out, in)
		}
	}
}

func TestParticipantsCursor_Empty(t *testing.T) {
	out, err := decodeParticipantsCursor("", 42)
	if err != nil || out != (participantsCursor{}) {
		t.Fatalf("got %+v, %v; want zero, nil", out, err)
	}
}

func TestParticipantsCursor_WrongChat(t *testing.T) {
	s := encodeParticipantsCursor(participantsCursor{ChatID: 42, Source: SourceMembers, Offset: 5})
	if _, err := decodeParticipantsCursor(s, 43); !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("err = %v, want ErrInvalidCursor", err)
	}
}

func TestParticipantsCursor_Garbage(t *testing.T) {
	enc := base64.RawURLEncoding.EncodeToString
	for _, s := range []string{"!!!", enc([]byte("{}")), enc([]byte(`{"c":42,"s":"x"}`))} {
		if _, err := decodeParticipantsCursor(s, 42); !errors.Is(err, ErrInvalidCursor) {
			t.Errorf("decode(%q) err = %v, want ErrInvalidCursor", s, err)
		}
	}
}
