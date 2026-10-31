package tg

import (
	"context"
	"fmt"

	tgapi "github.com/gotd/td/tg"
)

func (s *Service) ReadChat(ctx context.Context, p ReadChatParams) (*ReadChatResult, error) {
	if p.Limit == 0 {
		p.Limit = DefaultLimit
	}
	peer, err := s.resolvePeer(ctx, p.ChatType, p.ChatID)
	if err != nil {
		return nil, err
	}

	resp, err := s.api.MessagesGetHistory(ctx, &tgapi.MessagesGetHistoryRequest{
		Peer:     peer,
		OffsetID: p.BeforeID,
		Limit:    p.Limit,
	})
	if err != nil {
		return nil, fmt.Errorf("messages.getHistory: %w", err)
	}

	var (
		messages []tgapi.MessageClass
		users    []tgapi.UserClass
		chats    []tgapi.ChatClass
	)
	switch r := resp.(type) {
	case *tgapi.MessagesMessages:
		messages, users, chats = r.Messages, r.Users, r.Chats
	case *tgapi.MessagesMessagesSlice:
		messages, users, chats = r.Messages, r.Users, r.Chats
	case *tgapi.MessagesChannelMessages:
		messages, users, chats = r.Messages, r.Users, r.Chats
	default: // *tgapi.MessagesMessagesNotModified
		return &ReadChatResult{Messages: []Message{}}, nil
	}

	s.peers.PutFromResponse(users, chats)

	names := NewNames(users, chats)
	peerTitle := names.peerTitle(p.ChatType, p.ChatID)
	result := &ReadChatResult{Messages: []Message{}}
	cutOff := false
	for _, mc := range messages { // newest first
		m, ok := RenderMessage(mc, names, peerTitle)
		if !ok {
			continue // MessageEmpty
		}
		if p.SinceDate > 0 && m.Date < p.SinceDate {
			cutOff = true
			break
		}
		result.Messages = append(result.Messages, m)
	}

	if !cutOff && len(result.Messages) > 0 && len(messages) >= p.Limit {
		next := result.Messages[len(result.Messages)-1].ID
		result.NextBeforeID = &next
	}
	return result, nil
}

// resolvePeer returns the input peer for a chat from the peer cache.
func (s *Service) resolvePeer(_ context.Context, t ChatType, id int64) (tgapi.InputPeerClass, error) {
	if peer, ok := s.peers.Get(t, id); ok {
		return peer, nil
	}
	return nil, ErrUnknownChat
}

// peerTitle returns the name of the requested chat, or "" when the response
// does not contain it. PeerName alone would say "Deleted Account" for a
// private chat whose user is absent.
func (n *Names) peerTitle(t ChatType, id int64) string {
	switch t {
	case ChatPrivate:
		if _, ok := n.users[id]; !ok {
			return ""
		}
		return n.PeerName(&tgapi.PeerUser{UserID: id})
	case ChatGroup:
		return n.PeerName(&tgapi.PeerChat{ChatID: id})
	case ChatSupergroup, ChatChannel:
		return n.PeerName(&tgapi.PeerChannel{ChannelID: id})
	}
	return ""
}
