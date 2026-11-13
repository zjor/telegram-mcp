package tg

import (
	"context"
	"errors"
	"fmt"

	tgapi "github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
)

const (
	historyPageSize = 100
	maxScanPerCall  = 1000
)

// hiddenList answers when the member list is hidden from the owner.
// filter=admins asks Telegram for the admin list, which is complete when
// allowed. Everything else scans recent history for senders.
func (s *Service) hiddenList(ctx context.Context, peer tgapi.InputPeerClass, p ListParticipantsParams, cur participantsCursor) (*ListParticipantsResult, error) {
	if p.Filter == FilterAdmins {
		if _, ok := peer.(*tgapi.InputPeerChannel); !ok {
			return nil, ErrAdminsHidden
		}
		if cur.Source != SourceMembers {
			cur = participantsCursor{}
		}
		res, err := s.channelMembers(ctx, peer, p, cur.Offset, nil)
		if errors.Is(err, ErrNotAMember) || tgerr.Is(err, "CHAT_ADMIN_REQUIRED") {
			return nil, ErrAdminsHidden
		}
		return res, err
	}
	if cur.Source != SourceRecentSenders {
		cur = participantsCursor{}
	}
	return s.recentSenders(ctx, peer, p, cur.BeforeID)
}

// recentSenders scans history below beforeID (0 = newest) and returns unique
// user senders, newest first. It stops at p.Limit senders, maxScanPerCall
// messages, or the end of history.
func (s *Service) recentSenders(ctx context.Context, peer tgapi.InputPeerClass, p ListParticipantsParams, beforeID int) (*ListParticipantsResult, error) {
	res := &ListParticipantsResult{Source: SourceRecentSenders, Participants: []Participant{}}
	seen := map[int64]bool{}
	next := func(id int) *ListParticipantsResult {
		c := encodeParticipantsCursor(participantsCursor{ChatID: p.ChatID, Source: SourceRecentSenders, BeforeID: id})
		res.NextCursor = &c
		return res
	}

	offsetID := beforeID
	for scanned := 0; scanned < maxScanPerCall; {
		msgs, users, ended, err := s.historyPage(ctx, peer, offsetID)
		if err != nil {
			return nil, err
		}
		for _, mc := range msgs {
			scanned++
			offsetID = mc.GetID()
			u, date, ok := userSender(mc, users)
			if !ok || seen[u.ID] {
				continue
			}
			seen[u.ID] = true
			if (p.Filter == FilterBots && !u.Bot) || !matchesQuery(u, p.Query) {
				continue
			}
			part := newParticipant(u)
			part.LastMessageDate = int64(date)
			res.Participants = append(res.Participants, part)
			if len(res.Participants) == p.Limit {
				return next(offsetID), nil
			}
		}
		if ended || len(msgs) == 0 {
			return res, nil
		}
	}
	return next(offsetID), nil
}

// historyPage loads one page below offsetID and caches its peers. ended is
// true when Telegram signals there is nothing older.
func (s *Service) historyPage(ctx context.Context, peer tgapi.InputPeerClass, offsetID int) ([]tgapi.MessageClass, map[int64]*tgapi.User, bool, error) {
	resp, err := s.api.MessagesGetHistory(ctx, &tgapi.MessagesGetHistoryRequest{Peer: peer, OffsetID: offsetID, Limit: historyPageSize})
	if err != nil {
		return nil, nil, false, wrapAccessErr("messages.getHistory", err)
	}
	var (
		msgs     []tgapi.MessageClass
		users    []tgapi.UserClass
		chats    []tgapi.ChatClass
		complete bool
	)
	switch r := resp.(type) {
	case *tgapi.MessagesMessages:
		msgs, users, chats, complete = r.Messages, r.Users, r.Chats, true
	case *tgapi.MessagesMessagesSlice:
		msgs, users, chats = r.Messages, r.Users, r.Chats
	case *tgapi.MessagesChannelMessages:
		msgs, users, chats = r.Messages, r.Users, r.Chats
	case *tgapi.MessagesMessagesNotModified:
		return nil, nil, true, nil
	default:
		return nil, nil, false, fmt.Errorf("messages.getHistory: unexpected type %T", resp)
	}
	s.cacheAndSave(users, chats)
	return msgs, usersByID(users), complete || len(msgs) < historyPageSize, nil
}

// userSender returns the user who sent a message or service message. Channel
// senders (anonymous admins, send-as-channel) and empty messages give ok=false.
func userSender(mc tgapi.MessageClass, users map[int64]*tgapi.User) (*tgapi.User, int, bool) {
	var (
		from tgapi.PeerClass
		date int
	)
	switch m := mc.(type) {
	case *tgapi.Message:
		from, _ = m.GetFromID()
		date = m.Date
	case *tgapi.MessageService:
		from, _ = m.GetFromID()
		date = m.Date
	default:
		return nil, 0, false
	}
	pu, ok := from.(*tgapi.PeerUser)
	if !ok {
		return nil, 0, false
	}
	u, ok := users[pu.UserID]
	return u, date, ok
}
