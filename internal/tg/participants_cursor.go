package tg

import (
	"encoding/base64"
	"encoding/json"
)

// participantsCursor is the opaque list_participants page token. It is bound
// to one chat so a cursor cannot be replayed against another.
type participantsCursor struct {
	ChatID   int64             `json:"c"`
	Source   ParticipantSource `json:"s"`
	Offset   int               `json:"o,omitempty"` // members source
	BeforeID int               `json:"b,omitempty"` // recent_senders source
}

func encodeParticipantsCursor(c participantsCursor) string {
	data, _ := json.Marshal(c) // cannot fail: plain struct of ints and strings
	return base64.RawURLEncoding.EncodeToString(data)
}

// decodeParticipantsCursor returns the zero cursor for "". Anything that does
// not decode, has an unknown source, or belongs to another chat is
// ErrInvalidCursor.
func decodeParticipantsCursor(s string, chatID int64) (participantsCursor, error) {
	if s == "" {
		return participantsCursor{}, nil
	}
	data, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return participantsCursor{}, ErrInvalidCursor
	}
	var c participantsCursor
	if err := json.Unmarshal(data, &c); err != nil {
		return participantsCursor{}, ErrInvalidCursor
	}
	if c.Source != SourceMembers && c.Source != SourceRecentSenders {
		return participantsCursor{}, ErrInvalidCursor
	}
	if c.ChatID != chatID {
		return participantsCursor{}, ErrInvalidCursor
	}
	return c, nil
}
