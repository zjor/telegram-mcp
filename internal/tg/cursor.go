package tg

import (
	"encoding/base64"
	"encoding/json"
	"errors"
)

var ErrInvalidCursor = errors.New("invalid cursor")

// cursor is the opaque list_chats page token. It never holds an access hash:
// the offset peer is resolved from the peer cache.
type cursor struct {
	Date     int      `json:"d"`
	ID       int      `json:"i"`
	PeerType ChatType `json:"t"`
	PeerID   int64    `json:"p"`
	Folder   int      `json:"f"` // 0 main, 1 archive
	// FromTop marks the "exclude pinned, no offset" cursor: the next page
	// starts at the top of the unpinned dialogs. Date, ID, PeerType and
	// PeerID are then zero.
	FromTop bool `json:"x"`
}

func encodeCursor(c cursor) string {
	data, _ := json.Marshal(c) // cannot fail: plain struct of ints and strings
	return base64.RawURLEncoding.EncodeToString(data)
}

func decodeCursor(s string) (cursor, error) {
	data, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return cursor{}, ErrInvalidCursor
	}
	var c cursor
	if err := json.Unmarshal(data, &c); err != nil {
		return cursor{}, ErrInvalidCursor
	}
	switch c.PeerType {
	case ChatPrivate, ChatGroup, ChatSupergroup, ChatChannel:
		return c, nil
	case "":
		if c.FromTop {
			return c, nil
		}
	}
	return cursor{}, ErrInvalidCursor
}
