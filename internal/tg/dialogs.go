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
	folder := p.Folder
	if folder != FolderArchive {
		folder = FolderMain
	}
	folderID := 0
	if folder == FolderArchive {
		folderID = 1
	}
	var c *cursor
	if p.Cursor != "" {
		dc, err := decodeCursor(p.Cursor)
		if err != nil {
			return nil, err
		}
		if dc.Folder != folderID {
			return nil, ErrInvalidCursor
		}
		c = &dc
	}

	page, err := s.fetchDialogs(ctx, folderID, c, p.Limit)
	if err != nil {
		return nil, err
	}
	if page.notModified {
		return &ListChatsResult{Chats: []Chat{}}, nil
	}

	s.peers.PutFromResponse(page.users, page.chats)
	if err := s.peers.Save(); err != nil {
		slog.Warn("save peer cache", "err", err)
	}

	names := NewNames(page.users, page.chats)
	result := &ListChatsResult{Chats: []Chat{}}
	for _, d := range page.dialogs {
		chat := names.dialogChat(d.Peer)
		chat.LastMessageDate = int64(topMessageDate(page.messages, d.TopMessage, d.Peer))
		chat.UnreadCount = d.UnreadCount
		chat.Pinned = d.Pinned
		chat.Folder = folder
		result.Chats = append(result.Chats, chat)
	}

	if nc := nextCursor(folderID, page, names, c != nil); nc != nil {
		next := encodeCursor(*nc)
		result.NextCursor = &next
	}
	return result, nil
}

// nextCursor returns the cursor for the page after this one, or nil when the
// page is the last one. The offset comes from the last dialog that is not
// pinned and whose top message is in the response: pinned dialogs have
// arbitrary dates, and a zero date would restart the listing. hadCursor says
// whether the request for this page carried a cursor.
//
// When no dialog qualifies on the first page (hadCursor false, for example a
// pinned-only page), it returns the FromTop cursor, which restarts at the top
// of the unpinned dialogs. On a later page (hadCursor true) it returns nil and
// paging ends: that request already excluded pinned dialogs, so emitting
// FromTop again would restart the same unpinned list and the caller would
// loop forever.
//
// Every request with a cursor excludes pinned dialogs, so pinned dialogs
// appear only on the first page. Known limitation: pinned dialogs beyond the
// first page's limit are not listed. With the default limit 50 this cannot
// happen, because Telegram allows far fewer pins.
func nextCursor(folderID int, page dialogsPage, names *Names, hadCursor bool) *cursor {
	if page.complete {
		return nil
	}
	for i := len(page.dialogs) - 1; i >= 0; i-- {
		d := page.dialogs[i]
		if d.Pinned {
			continue
		}
		date := topMessageDate(page.messages, d.TopMessage, d.Peer)
		if date == 0 {
			continue
		}
		chat := names.dialogChat(d.Peer)
		return &cursor{Date: date, ID: d.TopMessage, PeerType: chat.Type, PeerID: chat.ID, Folder: folderID}
	}
	if hadCursor {
		return nil
	}
	return &cursor{Folder: folderID, FromTop: true}
}

// dialogsPage is one messages.getDialogs response. dialogs holds only
// *tgapi.Dialog entries (DialogFolder and others are dropped). complete means
// there is no next page: a full MessagesDialogs, a short or empty slice, or
// MessagesDialogsNotModified (which also sets notModified).
type dialogsPage struct {
	dialogs     []*tgapi.Dialog
	users       []tgapi.UserClass
	chats       []tgapi.ChatClass
	messages    []tgapi.MessageClass
	complete    bool
	notModified bool
}

// fetchDialogs requests one page of dialogs in the folder (0 main, 1 archive)
// starting after c (nil for the first page). Requests with a cursor exclude
// pinned dialogs. A FromTop cursor sends no offset. The offset peer is taken
// from the peer cache; it stays empty when the cache misses.
func (s *Service) fetchDialogs(ctx context.Context, folderID int, c *cursor, limit int) (dialogsPage, error) {
	req := &tgapi.MessagesGetDialogsRequest{Limit: limit, OffsetPeer: &tgapi.InputPeerEmpty{}}
	req.SetFolderID(folderID)
	if c != nil {
		req.SetExcludePinned(true)
	}
	if c != nil && !c.FromTop {
		req.OffsetDate = c.Date
		req.OffsetID = c.ID
		if peer, ok := s.peers.Get(c.PeerType, c.PeerID); ok {
			req.OffsetPeer = peer
		}
	}

	resp, err := s.api.MessagesGetDialogs(ctx, req)
	if err != nil {
		return dialogsPage{}, fmt.Errorf("messages.getDialogs: %w", err)
	}

	var (
		page dialogsPage
		raw  []tgapi.DialogClass
	)
	switch r := resp.(type) {
	case *tgapi.MessagesDialogs:
		raw, page.messages, page.users, page.chats, page.complete = r.Dialogs, r.Messages, r.Users, r.Chats, true
	case *tgapi.MessagesDialogsSlice:
		raw, page.messages, page.users, page.chats = r.Dialogs, r.Messages, r.Users, r.Chats
		page.complete = len(raw) == 0 || len(raw) < limit
	default: // *tgapi.MessagesDialogsNotModified
		return dialogsPage{complete: true, notModified: true}, nil
	}
	// Telegram adds every pinned dialog to the first page and rounds small
	// limits up, so cap the unpinned dialogs here and keep all pinned ones.
	// It also adds the main folder's pinned dialogs to the first archive
	// page; each dialog carries its real folder, so drop the ones that do
	// not belong to the requested folder. Their peers are still cached by
	// the caller from users and chats.
	unpinned := 0
	for _, dc := range raw {
		d, ok := dc.(*tgapi.Dialog)
		if !ok {
			continue
		}
		if f, _ := d.GetFolderID(); f != folderID {
			continue
		}
		if !d.Pinned {
			if unpinned == limit {
				continue
			}
			unpinned++
		}
		page.dialogs = append(page.dialogs, d)
	}
	return page, nil
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
