package tg

import (
	"os"
	"path/filepath"
	"testing"

	tgapi "github.com/gotd/td/tg"
)

func TestPeerCache_PutGet(t *testing.T) {
	c := NewPeerCache(filepath.Join(t.TempDir(), "peers.json"))
	c.Put(ChatPrivate, 7, 99)
	c.Put(ChatGroup, 5, 0)
	c.Put(ChatChannel, 3, 1)

	p, ok := c.Get(ChatPrivate, 7)
	if u, isU := p.(*tgapi.InputPeerUser); !ok || !isU || u.UserID != 7 || u.AccessHash != 99 {
		t.Fatalf("private: %#v %v", p, ok)
	}
	p, ok = c.Get(ChatGroup, 5)
	if g, isG := p.(*tgapi.InputPeerChat); !ok || !isG || g.ChatID != 5 {
		t.Fatalf("group: %#v %v", p, ok)
	}
	for _, typ := range []ChatType{ChatChannel, ChatSupergroup} {
		p, ok = c.Get(typ, 3)
		if ch, isC := p.(*tgapi.InputPeerChannel); !ok || !isC || ch.ChannelID != 3 || ch.AccessHash != 1 {
			t.Fatalf("%s: %#v %v", typ, p, ok)
		}
	}
}

func TestPeerCache_GetUnknown(t *testing.T) {
	c := NewPeerCache(filepath.Join(t.TempDir(), "peers.json"))
	if _, ok := c.Get(ChatPrivate, 1); ok {
		t.Fatal("expected not found")
	}
}

func TestPeerCache_SaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "peers.json")
	c := NewPeerCache(path)
	c.Put(ChatPrivate, 7, 99)
	c.Put(ChatSupergroup, 3, 4)
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", st.Mode().Perm())
	}
	ds, _ := os.Stat(filepath.Dir(path))
	if ds.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode %v", ds.Mode().Perm())
	}
	c2 := NewPeerCache(path)
	if err := c2.Load(); err != nil {
		t.Fatal(err)
	}
	p, ok := c2.Get(ChatPrivate, 7)
	if u, isU := p.(*tgapi.InputPeerUser); !ok || !isU || u.AccessHash != 99 {
		t.Fatalf("private: %#v %v", p, ok)
	}
	p, ok = c2.Get(ChatChannel, 3)
	if ch, isC := p.(*tgapi.InputPeerChannel); !ok || !isC || ch.AccessHash != 4 {
		t.Fatalf("channel: %#v %v", p, ok)
	}
}

func TestPeerCache_SaveEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "peers.json")
	if err := NewPeerCache(path).Save(); err != nil {
		t.Fatal(err)
	}
	c := NewPeerCache(path)
	if err := c.Load(); err != nil {
		t.Fatal(err)
	}
}

func TestPeerCache_LoadMissingFile(t *testing.T) {
	c := NewPeerCache(filepath.Join(t.TempDir(), "none.json"))
	if err := c.Load(); err != nil {
		t.Fatal(err)
	}
}

func TestPeerCache_LoadInvalidJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "peers.json")
	if err := os.WriteFile(path, []byte("{nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := NewPeerCache(path).Load(); err == nil {
		t.Fatal("expected error")
	}
}

func TestPeerCache_PutFromResponse(t *testing.T) {
	c := NewPeerCache(filepath.Join(t.TempDir(), "peers.json"))
	c.PutFromResponse(
		[]tgapi.UserClass{
			&tgapi.User{ID: 7, AccessHash: 9},
			&tgapi.User{ID: 8, Min: true, AccessHash: 1},
			&tgapi.UserEmpty{ID: 9},
		},
		[]tgapi.ChatClass{
			&tgapi.Channel{ID: 3, AccessHash: 4, Megagroup: true},
			&tgapi.Channel{ID: 4, AccessHash: 5},
			&tgapi.Channel{ID: 6, AccessHash: 5, Min: true},
			&tgapi.ChannelForbidden{ID: 10, AccessHash: 2},
			&tgapi.Chat{ID: 5},
			&tgapi.ChatForbidden{ID: 11},
			&tgapi.ChatEmpty{ID: 12},
		},
	)
	for _, tc := range []struct {
		t  ChatType
		id int64
		ok bool
	}{
		{ChatPrivate, 7, true}, {ChatPrivate, 8, false}, {ChatPrivate, 9, false},
		{ChatSupergroup, 3, true}, {ChatChannel, 4, true}, {ChatChannel, 6, false},
		{ChatChannel, 10, true}, {ChatGroup, 5, true}, {ChatGroup, 11, true}, {ChatGroup, 12, false},
	} {
		if _, ok := c.Get(tc.t, tc.id); ok != tc.ok {
			t.Errorf("%s:%d ok=%v want %v", tc.t, tc.id, ok, tc.ok)
		}
	}
}
