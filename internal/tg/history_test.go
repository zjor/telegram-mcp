package tg

import (
	"context"
	"errors"
	"testing"

	"github.com/gotd/td/bin"
	tgapi "github.com/gotd/td/tg"
)

func historyRequest(t *testing.T, b bin.Encoder) *tgapi.MessagesGetHistoryRequest {
	t.Helper()
	req, ok := b.(*tgapi.MessagesGetHistoryRequest)
	if !ok {
		t.Fatalf("request type %T, want *MessagesGetHistoryRequest", b)
	}
	return req
}

// assertHistoryRequest checks OffsetID and Limit, and that every other offset field is zero.
func assertHistoryRequest(t *testing.T, req *tgapi.MessagesGetHistoryRequest, offsetID, limit int) {
	t.Helper()
	if req.OffsetID != offsetID || req.Limit != limit {
		t.Errorf("OffsetID/Limit = %d/%d, want %d/%d", req.OffsetID, req.Limit, offsetID, limit)
	}
	if req.OffsetDate != 0 || req.AddOffset != 0 || req.MaxID != 0 || req.MinID != 0 || req.Hash != 0 {
		t.Errorf("unexpected non-zero fields: %+v", req)
	}
}

func textMsg(id, date int, from tgapi.PeerClass, text string) *tgapi.Message {
	m := &tgapi.Message{ID: id, Date: date, Message: text, PeerID: &tgapi.PeerUser{UserID: 7}}
	if from != nil {
		m.SetFromID(from)
	}
	return m
}

func twoMessages() []tgapi.MessageClass {
	out := textMsg(30, 300, nil, "hi")
	out.Out = true
	return []tgapi.MessageClass{out, textMsg(20, 200, &tgapi.PeerUser{UserID: 7}, "yo")}
}

func annUsers() []tgapi.UserClass {
	return []tgapi.UserClass{&tgapi.User{ID: 7, AccessHash: 9, FirstName: "Ann"}}
}

func assertMessages(t *testing.T, got, want []Message) {
	t.Helper()
	if got == nil {
		t.Fatal("Messages is nil, want non-nil slice")
	}
	if len(got) != len(want) {
		t.Fatalf("got %d messages %+v, want %d %+v", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("message %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestReadChat_RendersPage(t *testing.T) {
	s, mock, _ := newTestService(t)
	s.peers.Put(ChatPrivate, 7, 9)
	mock.ExpectFunc(func(b bin.Encoder) {
		req := historyRequest(t, b)
		peer, ok := req.Peer.(*tgapi.InputPeerUser)
		if !ok || peer.UserID != 7 || peer.AccessHash != 9 {
			t.Errorf("Peer = %#v, want InputPeerUser{7, 9}", req.Peer)
		}
		assertHistoryRequest(t, req, 0, 50)
	}).ThenResult(&tgapi.MessagesMessagesSlice{Count: 2, Messages: twoMessages(), Users: annUsers()})

	res, err := s.ReadChat(context.Background(), ReadChatParams{ChatID: 7, ChatType: ChatPrivate})
	if err != nil {
		t.Fatal(err)
	}
	assertMessages(t, res.Messages, []Message{{30, 300, "me", "hi"}, {20, 200, "Ann", "yo"}})
	if res.NextBeforeID != nil {
		t.Errorf("NextBeforeID = %d, want nil", *res.NextBeforeID)
	}
}

func TestReadChat_NextBeforeID(t *testing.T) {
	s, mock, _ := newTestService(t)
	s.peers.Put(ChatPrivate, 7, 9)
	mock.ExpectFunc(func(b bin.Encoder) {
		assertHistoryRequest(t, historyRequest(t, b), 0, 2)
	}).ThenResult(&tgapi.MessagesMessagesSlice{Count: 10, Messages: twoMessages(), Users: annUsers()})

	res, err := s.ReadChat(context.Background(), ReadChatParams{ChatID: 7, ChatType: ChatPrivate, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if res.NextBeforeID == nil || *res.NextBeforeID != 20 {
		t.Fatalf("NextBeforeID = %v, want 20", res.NextBeforeID)
	}

	mock.ExpectFunc(func(b bin.Encoder) {
		assertHistoryRequest(t, historyRequest(t, b), 20, 2)
	}).ThenResult(&tgapi.MessagesMessagesSlice{Count: 10})
	if _, err := s.ReadChat(context.Background(), ReadChatParams{ChatID: 7, ChatType: ChatPrivate, Limit: 2, BeforeID: *res.NextBeforeID}); err != nil {
		t.Fatal(err)
	}
}

func TestReadChat_SinceDateCutsPage(t *testing.T) {
	s, mock, _ := newTestService(t)
	s.peers.Put(ChatPrivate, 7, 9)
	mock.ExpectFunc(func(b bin.Encoder) { historyRequest(t, b) }).
		ThenResult(&tgapi.MessagesMessagesSlice{Count: 10, Messages: twoMessages(), Users: annUsers()})

	res, err := s.ReadChat(context.Background(), ReadChatParams{ChatID: 7, ChatType: ChatPrivate, Limit: 2, SinceDate: 250})
	if err != nil {
		t.Fatal(err)
	}
	assertMessages(t, res.Messages, []Message{{30, 300, "me", "hi"}})
	if res.NextBeforeID != nil {
		t.Errorf("NextBeforeID = %d, want nil", *res.NextBeforeID)
	}
}

func TestReadChat_SinceDateDropsAll(t *testing.T) {
	s, mock, _ := newTestService(t)
	s.peers.Put(ChatPrivate, 7, 9)
	mock.ExpectFunc(func(b bin.Encoder) { historyRequest(t, b) }).
		ThenResult(&tgapi.MessagesMessagesSlice{Count: 10, Messages: twoMessages(), Users: annUsers()})

	res, err := s.ReadChat(context.Background(), ReadChatParams{ChatID: 7, ChatType: ChatPrivate, Limit: 2, SinceDate: 999})
	if err != nil {
		t.Fatal(err)
	}
	assertMessages(t, res.Messages, []Message{})
	if res.NextBeforeID != nil {
		t.Errorf("NextBeforeID = %d, want nil", *res.NextBeforeID)
	}
}

func TestReadChat_SkipsMessageEmpty(t *testing.T) {
	s, mock, _ := newTestService(t)
	s.peers.Put(ChatPrivate, 7, 9)
	mock.ExpectFunc(func(b bin.Encoder) { historyRequest(t, b) }).
		ThenResult(&tgapi.MessagesMessagesSlice{
			Count:    10,
			Messages: []tgapi.MessageClass{textMsg(30, 300, &tgapi.PeerUser{UserID: 7}, "hi"), &tgapi.MessageEmpty{ID: 25}},
			Users:    annUsers(),
		})

	res, err := s.ReadChat(context.Background(), ReadChatParams{ChatID: 7, ChatType: ChatPrivate, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	assertMessages(t, res.Messages, []Message{{30, 300, "Ann", "hi"}})
	if res.NextBeforeID == nil || *res.NextBeforeID != 30 {
		t.Fatalf("NextBeforeID = %v, want 30", res.NextBeforeID)
	}
}

func TestReadChat_ChannelPeerTitle(t *testing.T) {
	s, mock, _ := newTestService(t)
	s.peers.Put(ChatChannel, 3, 1)
	mock.ExpectFunc(func(b bin.Encoder) {
		req := historyRequest(t, b)
		peer, ok := req.Peer.(*tgapi.InputPeerChannel)
		if !ok || peer.ChannelID != 3 || peer.AccessHash != 1 {
			t.Errorf("Peer = %#v, want InputPeerChannel{3, 1}", req.Peer)
		}
	}).ThenResult(&tgapi.MessagesChannelMessages{
		Count:    1,
		Messages: []tgapi.MessageClass{&tgapi.Message{ID: 5, Date: 50, Message: "news", PeerID: &tgapi.PeerChannel{ChannelID: 3}}},
		Chats:    []tgapi.ChatClass{&tgapi.Channel{ID: 3, AccessHash: 1, Title: "News", Broadcast: true, Photo: &tgapi.ChatPhotoEmpty{}}},
	})

	res, err := s.ReadChat(context.Background(), ReadChatParams{ChatID: 3, ChatType: ChatChannel})
	if err != nil {
		t.Fatal(err)
	}
	assertMessages(t, res.Messages, []Message{{5, 50, "News", "news"}})
}

func TestReadChat_DeletedSender(t *testing.T) {
	s, mock, _ := newTestService(t)
	s.peers.Put(ChatGroup, 5, 0)
	mock.ExpectFunc(func(b bin.Encoder) { historyRequest(t, b) }).
		ThenResult(&tgapi.MessagesMessages{
			Messages: []tgapi.MessageClass{textMsg(1, 10, &tgapi.PeerUser{UserID: 8}, "bye")},
			Chats:    []tgapi.ChatClass{&tgapi.Chat{ID: 5, Title: "Team", Photo: &tgapi.ChatPhotoEmpty{}}},
			Users:    []tgapi.UserClass{&tgapi.UserEmpty{ID: 8}},
		})

	res, err := s.ReadChat(context.Background(), ReadChatParams{ChatID: 5, ChatType: ChatGroup})
	if err != nil {
		t.Fatal(err)
	}
	assertMessages(t, res.Messages, []Message{{1, 10, "Deleted Account", "bye"}})
}

func TestReadChat_UnknownPeer(t *testing.T) {
	s, _, _ := newTestService(t) // no expectations: any RPC fails the test
	_, err := s.ReadChat(context.Background(), ReadChatParams{ChatID: 7, ChatType: ChatPrivate})
	if !errors.Is(err, ErrUnknownChat) {
		t.Fatalf("err = %v, want ErrUnknownChat", err)
	}
}

func TestReadChat_ServiceMessage(t *testing.T) {
	s, mock, _ := newTestService(t)
	s.peers.Put(ChatPrivate, 7, 9)
	mock.ExpectFunc(func(b bin.Encoder) { historyRequest(t, b) }).
		ThenResult(&tgapi.MessagesMessages{
			Messages: []tgapi.MessageClass{&tgapi.MessageService{
				ID: 4, Date: 40, PeerID: &tgapi.PeerUser{UserID: 7}, Action: &tgapi.MessageActionPinMessage{},
			}},
			Users: annUsers(),
		})

	res, err := s.ReadChat(context.Background(), ReadChatParams{ChatID: 7, ChatType: ChatPrivate})
	if err != nil {
		t.Fatal(err)
	}
	assertMessages(t, res.Messages, []Message{{4, 40, "Ann", "[service: message pinned]"}})
}

func TestReadChat_PeerTitleAbsent(t *testing.T) {
	s, mock, _ := newTestService(t)
	s.peers.Put(ChatPrivate, 7, 9)
	mock.ExpectFunc(func(b bin.Encoder) { historyRequest(t, b) }).
		ThenResult(&tgapi.MessagesMessages{Messages: []tgapi.MessageClass{textMsg(1, 10, nil, "hi")}})

	res, err := s.ReadChat(context.Background(), ReadChatParams{ChatID: 7, ChatType: ChatPrivate})
	if err != nil {
		t.Fatal(err)
	}
	assertMessages(t, res.Messages, []Message{{1, 10, "", "hi"}})
}
