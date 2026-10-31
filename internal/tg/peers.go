package tg

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	tgapi "github.com/gotd/td/tg"
)

// PeerCache remembers (type, id) -> access hash and persists it as JSON.
// It is safe for concurrent use.
type PeerCache struct {
	mu      sync.Mutex
	path    string
	entries map[string]peerEntry
}

type peerEntry struct {
	Type       ChatType `json:"type"`
	ID         int64    `json:"id"`
	AccessHash int64    `json:"access_hash,omitempty"`
}

func NewPeerCache(path string) *PeerCache {
	return &PeerCache{path: path, entries: map[string]peerEntry{}}
}

// peerKey maps supergroup and channel to the same key: both are Telegram channels.
func peerKey(t ChatType, id int64) string {
	if t == ChatSupergroup {
		t = ChatChannel
	}
	return fmt.Sprintf("%s:%d", t, id)
}

// Load reads the cache file. A missing file is not an error.
func (c *PeerCache) Load() error {
	data, err := os.ReadFile(c.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read peer cache: %w", err)
	}
	var entries map[string]peerEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return fmt.Errorf("parse peer cache %s: %w", c.path, err)
	}
	if entries == nil {
		entries = map[string]peerEntry{}
	}
	c.mu.Lock()
	c.entries = entries
	c.mu.Unlock()
	return nil
}

// Save writes the cache atomically (temp file + rename) with mode 0600.
func (c *PeerCache) Save() error {
	c.mu.Lock()
	data, err := json.Marshal(c.entries)
	c.mu.Unlock()
	if err != nil {
		return fmt.Errorf("encode peer cache: %w", err)
	}
	dir := filepath.Dir(c.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create peer cache dir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".peers-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmp.Name(), c.path); err != nil {
		return fmt.Errorf("rename peer cache: %w", err)
	}
	return nil
}

func (c *PeerCache) Put(t ChatType, id int64, accessHash int64) {
	if t == ChatSupergroup {
		t = ChatChannel
	}
	c.mu.Lock()
	c.entries[peerKey(t, id)] = peerEntry{Type: t, ID: id, AccessHash: accessHash}
	c.mu.Unlock()
}

func (c *PeerCache) Get(t ChatType, id int64) (tgapi.InputPeerClass, bool) {
	c.mu.Lock()
	e, ok := c.entries[peerKey(t, id)]
	c.mu.Unlock()
	if !ok {
		return nil, false
	}
	switch e.Type {
	case ChatPrivate:
		return &tgapi.InputPeerUser{UserID: e.ID, AccessHash: e.AccessHash}, true
	case ChatGroup:
		return &tgapi.InputPeerChat{ChatID: e.ID}, true
	case ChatChannel, ChatSupergroup:
		return &tgapi.InputPeerChannel{ChannelID: e.ID, AccessHash: e.AccessHash}, true
	}
	return nil, false
}

// PutFromResponse stores peers from an API response. Min users and channels
// carry no usable access hash, so they are skipped.
func (c *PeerCache) PutFromResponse(users []tgapi.UserClass, chats []tgapi.ChatClass) {
	for _, u := range users {
		if user, ok := u.(*tgapi.User); ok && !user.Min {
			c.Put(ChatPrivate, user.ID, user.AccessHash)
		}
	}
	for _, ch := range chats {
		switch v := ch.(type) {
		case *tgapi.Chat:
			c.Put(ChatGroup, v.ID, 0)
		case *tgapi.ChatForbidden:
			c.Put(ChatGroup, v.ID, 0)
		case *tgapi.Channel:
			if v.Min {
				continue
			}
			t := ChatChannel
			if v.Megagroup {
				t = ChatSupergroup
			}
			c.Put(t, v.ID, v.AccessHash)
		case *tgapi.ChannelForbidden:
			c.Put(ChatChannel, v.ID, v.AccessHash)
		}
	}
}
