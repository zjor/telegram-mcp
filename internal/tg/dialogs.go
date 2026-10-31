package tg

import (
	"context"
	"fmt"
	"log/slog"

	tgapi "github.com/gotd/td/tg"
)

func (s *Service) ListChats(ctx context.Context, p ListChatsParams) (*ListChatsResult, error) {
	if p.Limit == 0 {
		p.Limit = DefaultLimit
	}
	req := &tgapi.MessagesGetDialogsRequest{Limit: p.Limit, OffsetPeer: &tgapi.InputPeerEmpty{}}
	if p.Folder == FolderArchive {
		req.SetFolderID(1)
	} else {
		req.SetFolderID(0)
	}
	if p.Cursor != "" {
		c, err := decodeCursor(p.Cursor)
		if err != nil {
			return nil, err
		}
		req.OffsetDate = c.Date
		req.OffsetID = c.ID
		if peer, ok := s.peers.Get(c.PeerType, c.PeerID); ok {
			req.OffsetPeer = peer
		}
	}

	resp, err := s.api.MessagesGetDialogs(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("messages.getDialogs: %w", err)
	}

	var (
		dialogs  []tgapi.DialogClass
		messages []tgapi.MessageClass
		users    []tgapi.UserClass
		chats    []tgapi.ChatClass
		complete bool
	)
	switch r := resp.(type) {
	case *tgapi.MessagesDialogs:
		dialogs, messages, users, chats, complete = r.Dialogs, r.Messages, r.Users, r.Chats, true
	case *tgapi.MessagesDialogsSlice:
		dialogs, messages, users, chats = r.Dialogs, r.Messages, r.Users, r.Chats
	default: // *tgapi.MessagesDialogsNotModified
		return &ListChatsResult{Chats: []Chat{}}, nil
	}

	s.peers.PutFromResponse(users, chats)
	if err := s.peers.Save(); err != nil {
		slog.Warn("save peer cache", "err", err)
	}

	names := NewNames(users, chats)
	folder := p.Folder
	if folder != FolderArchive {
		folder = FolderMain
	}
	result := &ListChatsResult{Chats: []Chat{}}
	var last *cursor
	for _, dc := range dialogs {
		d, ok := dc.(*tgapi.Dialog)
		if !ok {
			continue // DialogFolder, DialogCommunity
		}
		chat := names.dialogChat(d.Peer)
		date := topMessageDate(messages, d.TopMessage, d.Peer)
		chat.LastMessageDate = int64(date)
		chat.UnreadCount = d.UnreadCount
		chat.Folder = folder
		result.Chats = append(result.Chats, chat)
		last = &cursor{Date: date, ID: d.TopMessage, PeerType: chat.Type, PeerID: chat.ID}
	}

	if !complete && len(dialogs) >= p.Limit && last != nil {
		next := encodeCursor(*last)
		result.NextCursor = &next
	}
	return result, nil
}

// dialogChat fills ID, Type, Title and Username for a dialog peer.
func (n *Names) dialogChat(peer tgapi.PeerClass) Chat {
	switch p := peer.(type) {
	case *tgapi.PeerUser:
		c := Chat{ID: p.UserID, Type: ChatPrivate, Title: n.PeerName(p)}
		if u, ok := n.users[p.UserID]; ok {
			c.Username = u.Username
		}
		return c
	case *tgapi.PeerChat:
		return Chat{ID: p.ChatID, Type: ChatGroup, Title: n.PeerName(p)}
	case *tgapi.PeerChannel:
		c := Chat{ID: p.ChannelID, Type: ChatChannel, Title: n.PeerName(p)}
		switch ch := n.channels[p.ChannelID].(type) {
		case *tgapi.Channel:
			c.Username = ch.Username
			if ch.Megagroup {
				c.Type = ChatSupergroup
			}
		case *tgapi.ChannelForbidden:
			if ch.Megagroup {
				c.Type = ChatSupergroup
			}
		}
		return c
	}
	return Chat{}
}

// topMessageDate returns the date of the message with the given ID in the
// given peer, or 0 when the response does not contain it.
func topMessageDate(messages []tgapi.MessageClass, id int, peer tgapi.PeerClass) int {
	for _, mc := range messages {
		var (
			date   int
			msgPID tgapi.PeerClass
		)
		switch m := mc.(type) {
		case *tgapi.Message:
			if m.ID != id {
				continue
			}
			date, msgPID = m.Date, m.PeerID
		case *tgapi.MessageService:
			if m.ID != id {
				continue
			}
			date, msgPID = m.Date, m.PeerID
		default:
			continue
		}
		if samePeer(msgPID, peer) {
			return date
		}
	}
	return 0
}

func samePeer(a, b tgapi.PeerClass) bool {
	switch a := a.(type) {
	case *tgapi.PeerUser:
		b, ok := b.(*tgapi.PeerUser)
		return ok && a.UserID == b.UserID
	case *tgapi.PeerChat:
		b, ok := b.(*tgapi.PeerChat)
		return ok && a.ChatID == b.ChatID
	case *tgapi.PeerChannel:
		b, ok := b.(*tgapi.PeerChannel)
		return ok && a.ChannelID == b.ChannelID
	}
	return false
}
