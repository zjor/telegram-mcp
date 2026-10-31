package tg

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/gotd/td/bin"
	tgapi "github.com/gotd/td/tg"
	"github.com/gotd/td/tgmock"
)

func pinned(d *tgapi.Dialog) *tgapi.Dialog {
	d.Pinned = true
	return d
}

func newTestService(t *testing.T) (*Service, *tgmock.Mock, string) {
	t.Helper()
	mock := tgmock.New(t)
	path := filepath.Join(t.TempDir(), "peers.json")
	return NewService(mock, NewPeerCache(path)), mock, path
}

func dialogsRequest(t *testing.T, b bin.Encoder) *tgapi.MessagesGetDialogsRequest {
	t.Helper()
	req, ok := b.(*tgapi.MessagesGetDialogsRequest)
	if !ok {
		t.Fatalf("request type %T, want *MessagesGetDialogsRequest", b)
	}
	return req
}

func assertFolderID(t *testing.T, req *tgapi.MessagesGetDialogsRequest, want int) {
	t.Helper()
	got, ok := req.GetFolderID()
	if !ok || got != want {
		t.Errorf("FolderID = %d (set %v), want %d (set)", got, ok, want)
	}
}

func assertEmptyOffsetPeer(t *testing.T, req *tgapi.MessagesGetDialogsRequest) {
	t.Helper()
	if _, ok := req.OffsetPeer.(*tgapi.InputPeerEmpty); !ok {
		t.Errorf("OffsetPeer = %T, want *InputPeerEmpty", req.OffsetPeer)
	}
}

func dlg(peer tgapi.PeerClass, top, unread int) *tgapi.Dialog {
	return &tgapi.Dialog{Peer: peer, TopMessage: top, UnreadCount: unread, NotifySettings: tgapi.PeerNotifySettings{}}
}

func msg(id, date int, peer tgapi.PeerClass) *tgapi.Message {
	return &tgapi.Message{ID: id, Date: date, PeerID: peer}
}

func TestListChats_MapsAllPeerTypes(t *testing.T) {
	s, mock, _ := newTestService(t)
	pu, pc, psg, pch := &tgapi.PeerUser{UserID: 7}, &tgapi.PeerChat{ChatID: 5}, &tgapi.PeerChannel{ChannelID: 3}, &tgapi.PeerChannel{ChannelID: 4}
	mock.ExpectFunc(func(b bin.Encoder) {
		req := dialogsRequest(t, b)
		if req.Limit != 50 {
			t.Errorf("Limit = %d, want 50", req.Limit)
		}
		assertFolderID(t, req, 0)
		assertEmptyOffsetPeer(t, req)
	}).ThenResult(&tgapi.MessagesDialogsSlice{
		Count:   10,
		Dialogs: []tgapi.DialogClass{dlg(pu, 11, 1), dlg(pc, 12, 2), dlg(psg, 13, 3), dlg(pch, 14, 4)},
		Messages: []tgapi.MessageClass{
			msg(11, 100, pu), msg(12, 200, pc), msg(13, 300, psg), msg(14, 400, pch),
		},
		Chats: []tgapi.ChatClass{
			&tgapi.Chat{ID: 5, Title: "Team", Photo: &tgapi.ChatPhotoEmpty{}},
			&tgapi.Channel{ID: 3, AccessHash: 33, Title: "SG", Megagroup: true, Photo: &tgapi.ChatPhotoEmpty{}},
			&tgapi.Channel{ID: 4, AccessHash: 44, Title: "News", Broadcast: true, Photo: &tgapi.ChatPhotoEmpty{}},
		},
		Users: []tgapi.UserClass{&tgapi.User{ID: 7, AccessHash: 77, FirstName: "Ann", LastName: "Lee", Username: "ann"}},
	})

	res, err := s.ListChats(context.Background(), ListChatsParams{Folder: FolderMain})
	if err != nil {
		t.Fatal(err)
	}
	want := []Chat{
		{ID: 7, Type: ChatPrivate, Title: "Ann Lee", Username: "ann", LastMessageDate: 100, UnreadCount: 1, Folder: FolderMain},
		{ID: 5, Type: ChatGroup, Title: "Team", LastMessageDate: 200, UnreadCount: 2, Folder: FolderMain},
		{ID: 3, Type: ChatSupergroup, Title: "SG", LastMessageDate: 300, UnreadCount: 3, Folder: FolderMain},
		{ID: 4, Type: ChatChannel, Title: "News", LastMessageDate: 400, UnreadCount: 4, Folder: FolderMain},
	}
	if len(res.Chats) != len(want) {
		t.Fatalf("got %d chats, want %d: %+v", len(res.Chats), len(want), res.Chats)
	}
	for i := range want {
		if res.Chats[i] != want[i] {
			t.Errorf("chat %d = %+v, want %+v", i, res.Chats[i], want[i])
		}
	}
}

func TestListChats_ArchiveFolder(t *testing.T) {
	s, mock, _ := newTestService(t)
	pu := &tgapi.PeerUser{UserID: 7}
	mock.ExpectFunc(func(b bin.Encoder) {
		assertFolderID(t, dialogsRequest(t, b), 1)
	}).ThenResult(&tgapi.MessagesDialogs{
		Dialogs:  []tgapi.DialogClass{dlg(pu, 1, 0)},
		Messages: []tgapi.MessageClass{msg(1, 10, pu)},
		Users:    []tgapi.UserClass{&tgapi.User{ID: 7, FirstName: "Ann"}},
	})
	res, err := s.ListChats(context.Background(), ListChatsParams{Folder: FolderArchive})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Chats) != 1 || res.Chats[0].Folder != FolderArchive {
		t.Fatalf("chats = %+v, want one chat in folder archive", res.Chats)
	}
}

func TestListChats_SkipsDialogFolder(t *testing.T) {
	s, mock, _ := newTestService(t)
	pu := &tgapi.PeerUser{UserID: 7}
	mock.ExpectFunc(func(b bin.Encoder) { dialogsRequest(t, b) }).ThenResult(&tgapi.MessagesDialogs{
		Dialogs: []tgapi.DialogClass{
			&tgapi.DialogFolder{Folder: tgapi.Folder{ID: 1, Title: "Archived"}, Peer: &tgapi.PeerUser{UserID: 9}, TopMessage: 2},
			dlg(pu, 1, 0),
		},
		Messages: []tgapi.MessageClass{msg(1, 10, pu)},
		Users:    []tgapi.UserClass{&tgapi.User{ID: 7, FirstName: "Ann"}},
	})
	res, err := s.ListChats(context.Background(), ListChatsParams{Folder: FolderMain})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Chats) != 1 || res.Chats[0].ID != 7 {
		t.Fatalf("chats = %+v, want only chat 7", res.Chats)
	}
}

func TestListChats_MissingTopMessage(t *testing.T) {
	s, mock, _ := newTestService(t)
	pu := &tgapi.PeerUser{UserID: 7}
	mock.ExpectFunc(func(b bin.Encoder) { dialogsRequest(t, b) }).ThenResult(&tgapi.MessagesDialogs{
		Dialogs: []tgapi.DialogClass{dlg(pu, 1, 0)},
		// Same ID, different peer: must not match.
		Messages: []tgapi.MessageClass{msg(1, 999, &tgapi.PeerUser{UserID: 8})},
		Users:    []tgapi.UserClass{&tgapi.User{ID: 7, FirstName: "Ann"}},
	})
	res, err := s.ListChats(context.Background(), ListChatsParams{Folder: FolderMain})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Chats) != 1 || res.Chats[0].LastMessageDate != 0 {
		t.Fatalf("chats = %+v, want one chat with LastMessageDate 0", res.Chats)
	}
}

func TestListChats_DeletedUser(t *testing.T) {
	s, mock, _ := newTestService(t)
	pu := &tgapi.PeerUser{UserID: 7}
	mock.ExpectFunc(func(b bin.Encoder) { dialogsRequest(t, b) }).ThenResult(&tgapi.MessagesDialogs{
		Dialogs: []tgapi.DialogClass{dlg(pu, 1, 0)},
		Users:   []tgapi.UserClass{&tgapi.User{ID: 7, Deleted: true}},
	})
	res, err := s.ListChats(context.Background(), ListChatsParams{Folder: FolderMain})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Chats) != 1 || res.Chats[0].Title != "Deleted Account" {
		t.Fatalf("chats = %+v, want title Deleted Account", res.Chats)
	}
}

func TestListChats_NextCursor(t *testing.T) {
	s, mock, _ := newTestService(t)
	pc, pu := &tgapi.PeerChat{ChatID: 5}, &tgapi.PeerUser{UserID: 7}
	mock.ExpectFunc(func(b bin.Encoder) {
		req := dialogsRequest(t, b)
		if req.Limit != 2 {
			t.Errorf("Limit = %d, want 2", req.Limit)
		}
	}).ThenResult(&tgapi.MessagesDialogsSlice{
		Count:    10,
		Dialogs:  []tgapi.DialogClass{dlg(pc, 20, 0), dlg(pu, 21, 0)},
		Messages: []tgapi.MessageClass{msg(20, 500, pc), msg(21, 400, pu)},
		Chats:    []tgapi.ChatClass{&tgapi.Chat{ID: 5, Title: "Team", Photo: &tgapi.ChatPhotoEmpty{}}},
		Users:    []tgapi.UserClass{&tgapi.User{ID: 7, AccessHash: 777, FirstName: "Ann"}},
	})
	res, err := s.ListChats(context.Background(), ListChatsParams{Folder: FolderMain, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if res.NextCursor == nil {
		t.Fatal("NextCursor is nil, want a cursor")
	}
	c, err := decodeCursor(*res.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if want := (cursor{Date: 400, ID: 21, PeerType: ChatPrivate, PeerID: 7}); c != want {
		t.Fatalf("cursor = %+v, want %+v", c, want)
	}

	mock.ExpectFunc(func(b bin.Encoder) {
		req := dialogsRequest(t, b)
		if req.OffsetDate != 400 || req.OffsetID != 21 {
			t.Errorf("offset date/id = %d/%d, want 400/21", req.OffsetDate, req.OffsetID)
		}
		peer, ok := req.OffsetPeer.(*tgapi.InputPeerUser)
		if !ok || peer.UserID != 7 || peer.AccessHash != 777 {
			t.Errorf("OffsetPeer = %#v, want InputPeerUser{7, 777}", req.OffsetPeer)
		}
		assertFolderID(t, req, 0)
	}).ThenResult(&tgapi.MessagesDialogsSlice{Count: 10})
	if _, err := s.ListChats(context.Background(), ListChatsParams{Folder: FolderMain, Limit: 2, Cursor: *res.NextCursor}); err != nil {
		t.Fatal(err)
	}
}

func TestListChats_UncachedCursorPeer(t *testing.T) {
	s, mock, _ := newTestService(t)
	cur := encodeCursor(cursor{Date: 400, ID: 21, PeerType: ChatPrivate, PeerID: 7})
	mock.ExpectFunc(func(b bin.Encoder) {
		req := dialogsRequest(t, b)
		if req.OffsetDate != 400 || req.OffsetID != 21 {
			t.Errorf("offset date/id = %d/%d, want 400/21", req.OffsetDate, req.OffsetID)
		}
		assertEmptyOffsetPeer(t, req)
	}).ThenResult(&tgapi.MessagesDialogs{})
	if _, err := s.ListChats(context.Background(), ListChatsParams{Folder: FolderMain, Cursor: cur}); err != nil {
		t.Fatal(err)
	}
}

func TestListChats_LastPage(t *testing.T) {
	s, mock, _ := newTestService(t)
	pu, pc := &tgapi.PeerUser{UserID: 7}, &tgapi.PeerChat{ChatID: 5}
	users := []tgapi.UserClass{&tgapi.User{ID: 7, FirstName: "Ann"}}
	chats := []tgapi.ChatClass{&tgapi.Chat{ID: 5, Title: "Team", Photo: &tgapi.ChatPhotoEmpty{}}}

	// Complete list: no cursor even though the page is full.
	mock.ExpectFunc(func(b bin.Encoder) { dialogsRequest(t, b) }).ThenResult(&tgapi.MessagesDialogs{
		Dialogs: []tgapi.DialogClass{dlg(pu, 1, 0), dlg(pc, 2, 0)},
		Users:   users, Chats: chats,
	})
	res, err := s.ListChats(context.Background(), ListChatsParams{Folder: FolderMain, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if res.NextCursor != nil {
		t.Errorf("MessagesDialogs: NextCursor = %q, want nil", *res.NextCursor)
	}

	// Slice shorter than Limit: no cursor.
	mock.ExpectFunc(func(b bin.Encoder) { dialogsRequest(t, b) }).ThenResult(&tgapi.MessagesDialogsSlice{
		Count:   3,
		Dialogs: []tgapi.DialogClass{dlg(pu, 1, 0)},
		Users:   users,
	})
	res, err = s.ListChats(context.Background(), ListChatsParams{Folder: FolderMain, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if res.NextCursor != nil {
		t.Errorf("short slice: NextCursor = %q, want nil", *res.NextCursor)
	}
}

func TestListChats_NotModified(t *testing.T) {
	s, mock, _ := newTestService(t)
	mock.ExpectFunc(func(b bin.Encoder) { dialogsRequest(t, b) }).ThenResult(&tgapi.MessagesDialogsNotModified{Count: 3})
	res, err := s.ListChats(context.Background(), ListChatsParams{Folder: FolderMain})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Chats) != 0 || res.NextCursor != nil {
		t.Fatalf("result = %+v, want empty", res)
	}
}

func TestListChats_InvalidCursor(t *testing.T) {
	s, _, _ := newTestService(t) // no expectations: any RPC fails the test
	_, err := s.ListChats(context.Background(), ListChatsParams{Folder: FolderMain, Cursor: "zzz"})
	if !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("err = %v, want ErrInvalidCursor", err)
	}
}

func TestListChats_PersistsPeers(t *testing.T) {
	s, mock, path := newTestService(t)
	pu := &tgapi.PeerUser{UserID: 7}
	mock.ExpectFunc(func(b bin.Encoder) { dialogsRequest(t, b) }).ThenResult(&tgapi.MessagesDialogs{
		Dialogs: []tgapi.DialogClass{dlg(pu, 1, 0)},
		Users:   []tgapi.UserClass{&tgapi.User{ID: 7, AccessHash: 777, FirstName: "Ann"}},
	})
	if _, err := s.ListChats(context.Background(), ListChatsParams{Folder: FolderMain}); err != nil {
		t.Fatal(err)
	}
	fresh := NewPeerCache(path)
	if err := fresh.Load(); err != nil {
		t.Fatal(err)
	}
	peer, ok := fresh.Get(ChatPrivate, 7)
	if !ok {
		t.Fatal("private 7 not in reloaded cache")
	}
	if u, ok := peer.(*tgapi.InputPeerUser); !ok || u.AccessHash != 777 {
		t.Fatalf("peer = %#v, want InputPeerUser with access hash 777", peer)
	}
}

func TestListChats_DefaultLimit(t *testing.T) {
	s, mock, _ := newTestService(t)
	mock.ExpectFunc(func(b bin.Encoder) {
		if req := dialogsRequest(t, b); req.Limit != DefaultLimit {
			t.Errorf("Limit = %d, want %d", req.Limit, DefaultLimit)
		}
	}).ThenResult(&tgapi.MessagesDialogs{})
	if _, err := s.ListChats(context.Background(), ListChatsParams{Folder: FolderMain, Limit: 0}); err != nil {
		t.Fatal(err)
	}
}

func TestListChats_CursorSkipsDialogWithoutTopMessage(t *testing.T) {
	s, mock, _ := newTestService(t)
	pc, pu := &tgapi.PeerChat{ChatID: 5}, &tgapi.PeerUser{UserID: 7}
	mock.ExpectFunc(func(b bin.Encoder) { dialogsRequest(t, b) }).ThenResult(&tgapi.MessagesDialogsSlice{
		Count:    10,
		Dialogs:  []tgapi.DialogClass{dlg(pc, 20, 0), dlg(pu, 21, 0)},
		Messages: []tgapi.MessageClass{msg(20, 500, pc)}, // top message 21 of the last dialog is missing
		Chats:    []tgapi.ChatClass{&tgapi.Chat{ID: 5, Title: "Team", Photo: &tgapi.ChatPhotoEmpty{}}},
		Users:    []tgapi.UserClass{&tgapi.User{ID: 7, AccessHash: 777, FirstName: "Ann"}},
	})
	res, err := s.ListChats(context.Background(), ListChatsParams{Folder: FolderMain, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if res.NextCursor == nil {
		t.Fatal("NextCursor is nil, want a cursor")
	}
	c, err := decodeCursor(*res.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if want := (cursor{Date: 500, ID: 20, PeerType: ChatGroup, PeerID: 5}); c != want {
		t.Fatalf("cursor = %+v, want %+v", c, want)
	}
}

func TestListChats_CursorFolderMismatch(t *testing.T) {
	s, _, _ := newTestService(t) // no expectations: any RPC fails the test
	cur := encodeCursor(cursor{Date: 400, ID: 21, PeerType: ChatPrivate, PeerID: 7, Folder: 0})
	_, err := s.ListChats(context.Background(), ListChatsParams{Folder: FolderArchive, Cursor: cur})
	if !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("err = %v, want ErrInvalidCursor", err)
	}
}

func TestListChats_ExcludesPinnedOnLaterPages(t *testing.T) {
	s, mock, _ := newTestService(t)
	pc, pu := &tgapi.PeerChat{ChatID: 5}, &tgapi.PeerUser{UserID: 7}
	mock.ExpectFunc(func(b bin.Encoder) {
		if dialogsRequest(t, b).GetExcludePinned() {
			t.Error("first request: ExcludePinned set, want unset")
		}
	}).ThenResult(&tgapi.MessagesDialogsSlice{
		Count:    10,
		Dialogs:  []tgapi.DialogClass{pinned(dlg(pc, 20, 0)), dlg(pu, 21, 0)},
		Messages: []tgapi.MessageClass{msg(20, 900, pc), msg(21, 400, pu)},
		Chats:    []tgapi.ChatClass{&tgapi.Chat{ID: 5, Title: "Team", Photo: &tgapi.ChatPhotoEmpty{}}},
		Users:    []tgapi.UserClass{&tgapi.User{ID: 7, AccessHash: 777, FirstName: "Ann"}},
	})
	res, err := s.ListChats(context.Background(), ListChatsParams{Folder: FolderMain, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if res.NextCursor == nil {
		t.Fatal("NextCursor is nil, want a cursor")
	}
	mock.ExpectFunc(func(b bin.Encoder) {
		if !dialogsRequest(t, b).GetExcludePinned() {
			t.Error("second request: ExcludePinned unset, want set")
		}
	}).ThenResult(&tgapi.MessagesDialogsSlice{Count: 10})
	if _, err := s.ListChats(context.Background(), ListChatsParams{Folder: FolderMain, Limit: 2, Cursor: *res.NextCursor}); err != nil {
		t.Fatal(err)
	}
}

func TestListChats_PinnedOnlyPageYieldsTopCursor(t *testing.T) {
	s, mock, _ := newTestService(t)
	pc, pu := &tgapi.PeerChat{ChatID: 5}, &tgapi.PeerUser{UserID: 7}
	mock.ExpectFunc(func(b bin.Encoder) { dialogsRequest(t, b) }).ThenResult(&tgapi.MessagesDialogsSlice{
		Count:    10,
		Dialogs:  []tgapi.DialogClass{pinned(dlg(pc, 20, 0)), pinned(dlg(pu, 21, 0))},
		Messages: []tgapi.MessageClass{msg(20, 100, pc), msg(21, 900, pu)},
		Chats:    []tgapi.ChatClass{&tgapi.Chat{ID: 5, Title: "Team", Photo: &tgapi.ChatPhotoEmpty{}}},
		Users:    []tgapi.UserClass{&tgapi.User{ID: 7, AccessHash: 777, FirstName: "Ann"}},
	})
	res, err := s.ListChats(context.Background(), ListChatsParams{Folder: FolderMain, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if res.NextCursor == nil {
		t.Fatal("NextCursor is nil, want the top cursor")
	}
	c, err := decodeCursor(*res.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if want := (cursor{Folder: 0, FromTop: true}); c != want {
		t.Fatalf("cursor = %+v, want %+v", c, want)
	}

	mock.ExpectFunc(func(b bin.Encoder) {
		req := dialogsRequest(t, b)
		if req.OffsetDate != 0 || req.OffsetID != 0 {
			t.Errorf("OffsetDate/OffsetID = %d/%d, want 0/0", req.OffsetDate, req.OffsetID)
		}
		assertEmptyOffsetPeer(t, req)
		if !req.GetExcludePinned() {
			t.Error("ExcludePinned unset, want set")
		}
		assertFolderID(t, req, 0)
	}).ThenResult(&tgapi.MessagesDialogsSlice{Count: 10})
	if _, err := s.ListChats(context.Background(), ListChatsParams{Folder: FolderMain, Limit: 2, Cursor: *res.NextCursor}); err != nil {
		t.Fatal(err)
	}
}

func TestListChats_CursorFromLastUnpinned(t *testing.T) {
	s, mock, _ := newTestService(t)
	pc, pu := &tgapi.PeerChat{ChatID: 5}, &tgapi.PeerUser{UserID: 7}
	mock.ExpectFunc(func(b bin.Encoder) { dialogsRequest(t, b) }).ThenResult(&tgapi.MessagesDialogsSlice{
		Count:    10,
		Dialogs:  []tgapi.DialogClass{dlg(pc, 20, 0), pinned(dlg(pu, 21, 0))},
		Messages: []tgapi.MessageClass{msg(20, 500, pc), msg(21, 100, pu)},
		Chats:    []tgapi.ChatClass{&tgapi.Chat{ID: 5, Title: "Team", Photo: &tgapi.ChatPhotoEmpty{}}},
		Users:    []tgapi.UserClass{&tgapi.User{ID: 7, AccessHash: 777, FirstName: "Ann"}},
	})
	res, err := s.ListChats(context.Background(), ListChatsParams{Folder: FolderMain, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if res.NextCursor == nil {
		t.Fatal("NextCursor is nil, want a cursor")
	}
	c, err := decodeCursor(*res.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if want := (cursor{Date: 500, ID: 20, PeerType: ChatGroup, PeerID: 5}); c != want {
		t.Fatalf("cursor = %+v, want %+v", c, want)
	}
}

func TestListChats_LaterPageWithoutUsableDialogEndsPaging(t *testing.T) {
	s, mock, _ := newTestService(t)
	pc, pu := &tgapi.PeerChat{ChatID: 5}, &tgapi.PeerUser{UserID: 7}
	chats := []tgapi.ChatClass{&tgapi.Chat{ID: 5, Title: "Team", Photo: &tgapi.ChatPhotoEmpty{}}}
	users := []tgapi.UserClass{&tgapi.User{ID: 7, AccessHash: 777, FirstName: "Ann"}}
	mock.ExpectFunc(func(b bin.Encoder) { dialogsRequest(t, b) }).ThenResult(&tgapi.MessagesDialogsSlice{
		Count:    10,
		Dialogs:  []tgapi.DialogClass{dlg(pc, 20, 0), dlg(pu, 21, 0)},
		Messages: []tgapi.MessageClass{msg(20, 500, pc), msg(21, 400, pu)},
		Chats:    chats, Users: users,
	})
	res, err := s.ListChats(context.Background(), ListChatsParams{Folder: FolderMain, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if res.NextCursor == nil {
		t.Fatal("first page: NextCursor is nil, want a cursor")
	}

	// Later page: both top messages are missing, so no dialog gives an offset.
	mock.ExpectFunc(func(b bin.Encoder) { dialogsRequest(t, b) }).ThenResult(&tgapi.MessagesDialogsSlice{
		Count:   10,
		Dialogs: []tgapi.DialogClass{dlg(pc, 30, 0), dlg(pu, 31, 0)},
		Chats:   chats, Users: users,
	})
	res, err = s.ListChats(context.Background(), ListChatsParams{Folder: FolderMain, Limit: 2, Cursor: *res.NextCursor})
	if err != nil {
		t.Fatal(err)
	}
	if res.NextCursor != nil {
		c, _ := decodeCursor(*res.NextCursor)
		t.Fatalf("later page: NextCursor = %+v, want nil", c)
	}
}
