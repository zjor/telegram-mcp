package tg

import (
	"context"
	"fmt"
	"log/slog"

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

// Resolve scan limits: page size and the most pages read per folder.
const (
	resolvePageSize = 100
	resolveMaxPages = 20
)

// resolvePeer returns the input peer for a chat. On a cache miss it scans the
// main folder, then the archive folder, page by page, until the peer is cached.
func (s *Service) resolvePeer(ctx context.Context, t ChatType, id int64) (tgapi.InputPeerClass, error) {
	if peer, ok := s.peers.Get(t, id); ok {
		return peer, nil
	}
	peer, err := s.scanForPeer(ctx, t, id)
	if saveErr := s.peers.Save(); saveErr != nil {
		slog.Warn("save peer cache", "err", saveErr)
	}
	if err != nil {
		return nil, err
	}
	if peer == nil {
		return nil, ErrUnknownChat
	}
	return peer, nil
}

// scanForPeer pages through folders 0 and 1 and stores every page in the
// peer cache. It returns nil, nil when the peer is not found.
func (s *Service) scanForPeer(ctx context.Context, t ChatType, id int64) (tgapi.InputPeerClass, error) {
	for _, folderID := range []int{0, 1} {
		var c *cursor
		for i := 0; i < resolveMaxPages; i++ {
			page, err := s.fetchDialogs(ctx, folderID, c, resolvePageSize)
			if err != nil {
				return nil, err
			}
			s.peers.PutFromResponse(page.users, page.chats)
			if peer, ok := s.peers.Get(t, id); ok {
				return peer, nil
			}
			c = nextCursor(folderID, page, NewNames(page.users, page.chats))
			if c == nil {
				break
			}
		}
	}
	return nil, nil
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
