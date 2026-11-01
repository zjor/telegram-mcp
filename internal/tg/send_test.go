package tg

import (
	"context"
	"errors"
	"testing"

	"github.com/gotd/td/bin"
	tgapi "github.com/gotd/td/tg"
)

func sendRequest(t *testing.T, b bin.Encoder) *tgapi.MessagesSendMessageRequest {
	t.Helper()
	req, ok := b.(*tgapi.MessagesSendMessageRequest)
	if !ok {
		t.Fatalf("request type %T, want *MessagesSendMessageRequest", b)
	}
	return req
}

func TestSendMessage_MarkdownBecomesEntities(t *testing.T) {
	s, mock, _ := newTestService(t)
	s.peers.Put(ChatPrivate, 7, 9)
	mock.ExpectFunc(func(b bin.Encoder) {
		req := sendRequest(t, b)
		peer, ok := req.Peer.(*tgapi.InputPeerUser)
		if !ok || peer.UserID != 7 || peer.AccessHash != 9 {
			t.Errorf("Peer = %#v, want InputPeerUser{7, 9}", req.Peer)
		}
		if req.Message != "hello world" {
			t.Errorf("Message = %q, want %q", req.Message, "hello world")
		}
		if len(req.Entities) != 1 {
			t.Fatalf("Entities = %#v, want one bold entity", req.Entities)
		}
		bold, ok := req.Entities[0].(*tgapi.MessageEntityBold)
		if !ok || bold.Offset != 6 || bold.Length != 5 {
			t.Errorf("Entities[0] = %#v, want bold at 6 len 5", req.Entities[0])
		}
		if req.RandomID == 0 {
			t.Error("RandomID is zero")
		}
		if req.ReplyTo != nil {
			t.Errorf("ReplyTo = %#v, want nil", req.ReplyTo)
		}
	}).ThenResult(&tgapi.UpdateShortSentMessage{ID: 42, Date: 500})

	res, err := s.SendMessage(context.Background(), SendMessageParams{ChatID: 7, ChatType: ChatPrivate, Text: "hello **world**"})
	if err != nil {
		t.Fatal(err)
	}
	if res.ID != 42 || res.Date != 500 {
		t.Fatalf("result = %+v, want ID 42 Date 500", res)
	}
}

func TestSendMessage_ReplyTo(t *testing.T) {
	s, mock, _ := newTestService(t)
	s.peers.Put(ChatPrivate, 7, 9)
	mock.ExpectFunc(func(b bin.Encoder) {
		req := sendRequest(t, b)
		r, ok := req.ReplyTo.(*tgapi.InputReplyToMessage)
		if !ok || r.ReplyToMsgID != 10 {
			t.Errorf("ReplyTo = %#v, want InputReplyToMessage{10}", req.ReplyTo)
		}
	}).ThenResult(&tgapi.UpdateShortSentMessage{ID: 43, Date: 501})

	if _, err := s.SendMessage(context.Background(), SendMessageParams{ChatID: 7, ChatType: ChatPrivate, Text: "ok", ReplyTo: 10}); err != nil {
		t.Fatal(err)
	}
}

func TestSendMessage_ChannelUpdates(t *testing.T) {
	s, mock, _ := newTestService(t)
	s.peers.Put(ChatChannel, 5, 11)
	mock.ExpectFunc(func(b bin.Encoder) {
		req := sendRequest(t, b)
		peer, ok := req.Peer.(*tgapi.InputPeerChannel)
		if !ok || peer.ChannelID != 5 || peer.AccessHash != 11 {
			t.Errorf("Peer = %#v, want InputPeerChannel{5, 11}", req.Peer)
		}
	}).ThenResult(&tgapi.Updates{Updates: []tgapi.UpdateClass{
		&tgapi.UpdateMessageID{ID: 77},
		&tgapi.UpdateNewChannelMessage{Message: &tgapi.Message{ID: 77, Date: 600, Message: "ok", PeerID: &tgapi.PeerChannel{ChannelID: 5}}},
	}})

	res, err := s.SendMessage(context.Background(), SendMessageParams{ChatID: 5, ChatType: ChatSupergroup, Text: "ok"})
	if err != nil {
		t.Fatal(err)
	}
	if res.ID != 77 || res.Date != 600 {
		t.Fatalf("result = %+v, want ID 77 Date 600", res)
	}
}

func TestSendMessage_UnknownChat(t *testing.T) {
	s, mock, _ := newTestService(t)
	// Cache miss: resolvePeer scans main and archive folders, both empty.
	mock.ExpectFunc(func(b bin.Encoder) { dialogsRequest(t, b) }).ThenResult(&tgapi.MessagesDialogs{})
	mock.ExpectFunc(func(b bin.Encoder) { dialogsRequest(t, b) }).ThenResult(&tgapi.MessagesDialogs{})

	_, err := s.SendMessage(context.Background(), SendMessageParams{ChatID: 99, ChatType: ChatPrivate, Text: "ok"})
	if !errors.Is(err, ErrUnknownChat) {
		t.Fatalf("err = %v, want ErrUnknownChat", err)
	}
}
