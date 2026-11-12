package tg

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/gotd/td/bin"
	tgapi "github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
)

func fullUserResult(u *tgapi.User, f tgapi.UserFull) *tgapi.UsersUserFull {
	return &tgapi.UsersUserFull{FullUser: f, Users: []tgapi.UserClass{u}}
}

func expectGetFullUser(t *testing.T, id, hash int64) func(b bin.Encoder) {
	return func(b bin.Encoder) {
		t.Helper()
		req, ok := b.(*tgapi.UsersGetFullUserRequest)
		if !ok {
			t.Fatalf("request type %T, want *UsersGetFullUserRequest", b)
		}
		in, ok := req.ID.(*tgapi.InputUser)
		if !ok || in.UserID != id || in.AccessHash != hash {
			t.Errorf("ID = %#v, want InputUser{%d, %d}", req.ID, id, hash)
		}
	}
}

func TestGetUser_ByIDCacheHit(t *testing.T) {
	s, mock, _ := newTestService(t)
	s.peers.Put(ChatPrivate, 7, 9)
	mock.ExpectFunc(expectGetFullUser(t, 7, 9)).ThenResult(fullUserResult(
		&tgapi.User{ID: 7, AccessHash: 9, FirstName: "Ann"}, tgapi.UserFull{ID: 7, About: "bio"}))

	res, err := s.GetUser(context.Background(), GetUserParams{UserID: 7})
	if err != nil {
		t.Fatal(err)
	}
	if res.ID != 7 || res.FirstName != "Ann" || res.Bio != "bio" {
		t.Fatalf("res = %+v", res)
	}
	if !mock.AllWereMet() {
		t.Fatal("not all expected calls were made")
	}
}

func TestGetUser_ByIDCacheMissScansDialogs(t *testing.T) {
	s, mock, _ := newTestService(t)
	mock.ExpectFunc(func(b bin.Encoder) { dialogsRequest(t, b) }).ThenResult(&tgapi.MessagesDialogs{
		Dialogs: []tgapi.DialogClass{&tgapi.Dialog{Peer: &tgapi.PeerUser{UserID: 7}}},
		Users:   []tgapi.UserClass{&tgapi.User{ID: 7, AccessHash: 9, FirstName: "Ann"}},
	})
	mock.ExpectFunc(expectGetFullUser(t, 7, 9)).ThenResult(fullUserResult(
		&tgapi.User{ID: 7, AccessHash: 9, FirstName: "Ann"}, tgapi.UserFull{ID: 7}))

	res, err := s.GetUser(context.Background(), GetUserParams{UserID: 7})
	if err != nil {
		t.Fatal(err)
	}
	if res.ID != 7 {
		t.Fatalf("res = %+v", res)
	}
}

func TestGetUser_ByIDUnknown(t *testing.T) {
	s, mock, _ := newTestService(t)
	mock.ExpectFunc(func(b bin.Encoder) { dialogsRequest(t, b) }).ThenResult(&tgapi.MessagesDialogs{})
	mock.ExpectFunc(func(b bin.Encoder) { dialogsRequest(t, b) }).ThenResult(&tgapi.MessagesDialogs{})

	_, err := s.GetUser(context.Background(), GetUserParams{UserID: 99})
	if !errors.Is(err, ErrUnknownUser) {
		t.Fatalf("err = %v, want ErrUnknownUser", err)
	}
}

func resolveRequest(t *testing.T, b bin.Encoder, want string) {
	t.Helper()
	req, ok := b.(*tgapi.ContactsResolveUsernameRequest)
	if !ok {
		t.Fatalf("request type %T, want *ContactsResolveUsernameRequest", b)
	}
	if req.Username != want {
		t.Errorf("Username = %q, want %q", req.Username, want)
	}
}

func TestGetUser_ByUsername(t *testing.T) {
	s, mock, path := newTestService(t)
	mock.ExpectFunc(func(b bin.Encoder) { resolveRequest(t, b, "ann") }).ThenResult(&tgapi.ContactsResolvedPeer{
		Peer:  &tgapi.PeerUser{UserID: 7},
		Users: []tgapi.UserClass{&tgapi.User{ID: 7, AccessHash: 9}},
	})
	mock.ExpectFunc(expectGetFullUser(t, 7, 9)).ThenResult(fullUserResult(
		&tgapi.User{ID: 7, AccessHash: 9, Username: "ann"}, tgapi.UserFull{ID: 7}))

	res, err := s.GetUser(context.Background(), GetUserParams{Username: "ann"})
	if err != nil {
		t.Fatal(err)
	}
	if res.ID != 7 || res.Username != "ann" {
		t.Fatalf("res = %+v", res)
	}
	peer, ok := s.peers.Get(ChatPrivate, 7)
	if p, isUser := peer.(*tgapi.InputPeerUser); !ok || !isUser || p.AccessHash != 9 {
		t.Fatalf("peer cache = %#v, want InputPeerUser{7, 9}", peer)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("peer cache not saved: %v", err)
	}
}

func TestGetUser_UsernameIsChannel(t *testing.T) {
	s, mock, _ := newTestService(t)
	mock.ExpectFunc(func(b bin.Encoder) { resolveRequest(t, b, "news") }).ThenResult(&tgapi.ContactsResolvedPeer{
		Peer:  &tgapi.PeerChannel{ChannelID: 5},
		Chats: []tgapi.ChatClass{&tgapi.Channel{ID: 5, AccessHash: 1, Photo: &tgapi.ChatPhotoEmpty{}}},
	})

	_, err := s.GetUser(context.Background(), GetUserParams{Username: "news"})
	if !errors.Is(err, ErrNotAUser) {
		t.Fatalf("err = %v, want ErrNotAUser", err)
	}
}

func TestGetUser_UsernameNotFound(t *testing.T) {
	for _, typ := range []string{"USERNAME_NOT_OCCUPIED", "USERNAME_INVALID"} {
		s, mock, _ := newTestService(t)
		mock.ExpectFunc(func(b bin.Encoder) { resolveRequest(t, b, "nobody") }).
			ThenRPCErr(&tgerr.Error{Code: 400, Message: typ, Type: typ})

		_, err := s.GetUser(context.Background(), GetUserParams{Username: "nobody"})
		if !errors.Is(err, ErrUsernameNotFound) {
			t.Fatalf("%s: err = %v, want ErrUsernameNotFound", typ, err)
		}
	}
}

func TestGetUser_FullUserMissingFromUsers(t *testing.T) {
	s, mock, _ := newTestService(t)
	s.peers.Put(ChatPrivate, 7, 9)
	mock.ExpectFunc(expectGetFullUser(t, 7, 9)).ThenResult(&tgapi.UsersUserFull{FullUser: tgapi.UserFull{ID: 7}})

	if _, err := s.GetUser(context.Background(), GetUserParams{UserID: 7}); err == nil {
		t.Fatal("err = nil, want error")
	}
}
