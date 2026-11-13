package tg

import (
	"context"
	"errors"
	"fmt"
	"strings"

	tgapi "github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
)

var (
	ErrNotAMember   = errors.New("you are not a member of this chat")
	ErrAdminsHidden = errors.New("the group hides its admin list from members")
	ErrNotAGroup    = errors.New("list_participants supports group and supergroup only")
)

const (
	roleCreator = "creator"
	roleAdmin   = "admin"
	roleMember  = "member"
)

// ListParticipants lists members of a basic group or supergroup. When the
// member list is hidden from the owner, it falls back to recent message
// senders and marks the result with SourceRecentSenders.
func (s *Service) ListParticipants(ctx context.Context, p ListParticipantsParams) (*ListParticipantsResult, error) {
	if p.Limit == 0 {
		p.Limit = DefaultLimit
	}
	if p.Filter == "" {
		p.Filter = FilterAll
	}
	cur, err := decodeParticipantsCursor(p.Cursor, p.ChatID)
	if err != nil {
		return nil, err
	}

	peer, full, err := s.fullChat(ctx, p.ChatType, p.ChatID)
	if err != nil {
		return nil, err
	}

	switch fc := full.FullChat.(type) {
	case *tgapi.ChannelFull:
		self := findChannel(full.Chats, fc.ID)
		if self != nil && self.Broadcast {
			return nil, ErrNotAGroup
		}
		// The owner's admin rights override the hidden flag: the group
		// setting stays true for admins, but they can list members.
		isAdmin := false
		if self != nil {
			_, hasRights := self.GetAdminRights()
			isAdmin = self.Creator || hasRights
		}
		if !isAdmin && (!fc.CanViewParticipants || fc.ParticipantsHidden) {
			return s.hiddenList(ctx, peer, p, cur)
		}
		if cur.Source != SourceMembers {
			cur = participantsCursor{}
		}
		return s.channelMembers(ctx, peer, p, cur.Offset, &fc.ParticipantsCount)
	case *tgapi.ChatFull:
		list, ok := fc.Participants.(*tgapi.ChatParticipants)
		if !ok {
			return s.hiddenList(ctx, peer, p, cur)
		}
		if cur.Source != SourceMembers {
			cur = participantsCursor{}
		}
		return groupMembers(p, cur.Offset, list, usersByID(full.Users)), nil
	}
	return nil, fmt.Errorf("full chat: unexpected type %T", full.FullChat)
}

// fullChat resolves the peer and loads the full chat. A stale supergroup
// access hash is dropped and the call is retried once after a dialog rescan.
func (s *Service) fullChat(ctx context.Context, t ChatType, id int64) (tgapi.InputPeerClass, *tgapi.MessagesChatFull, error) {
	for attempt := 0; ; attempt++ {
		peer, err := s.resolvePeer(ctx, t, id)
		if err != nil {
			return nil, nil, err
		}
		var (
			full   *tgapi.MessagesChatFull
			method string
		)
		switch pp := peer.(type) {
		case *tgapi.InputPeerChannel:
			method = "channels.getFullChannel"
			full, err = s.api.ChannelsGetFullChannel(ctx, &tgapi.InputChannel{ChannelID: pp.ChannelID, AccessHash: pp.AccessHash})
		case *tgapi.InputPeerChat:
			method = "messages.getFullChat"
			full, err = s.api.MessagesGetFullChat(ctx, pp.ChatID)
		default:
			return nil, nil, fmt.Errorf("chat %d is not a group or supergroup", id)
		}
		if attempt == 0 && t == ChatSupergroup && tgerr.Is(err, "CHANNEL_INVALID", "PEER_ID_INVALID") {
			s.peers.Delete(t, id)
			continue
		}
		if err != nil {
			return nil, nil, wrapAccessErr(method, err)
		}
		s.cacheAndSave(full.Users, full.Chats)
		return peer, full, nil
	}
}

func wrapAccessErr(method string, err error) error {
	if tgerr.Is(err, "CHANNEL_PRIVATE", "CHAT_FORBIDDEN", "CHAT_ID_INVALID") {
		return ErrNotAMember
	}
	return fmt.Errorf("%s: %w", method, err)
}

// channelMembers lists one page of a supergroup. total nil means "use the
// count from the getParticipants response".
func (s *Service) channelMembers(ctx context.Context, peer tgapi.InputPeerClass, p ListParticipantsParams, offset int, total *int) (*ListParticipantsResult, error) {
	var filter tgapi.ChannelParticipantsFilterClass
	localQuery := p.Query
	switch {
	case p.Filter == FilterAdmins:
		filter = &tgapi.ChannelParticipantsAdmins{}
	case p.Filter == FilterBots:
		filter = &tgapi.ChannelParticipantsBots{}
	case p.Query != "":
		filter = &tgapi.ChannelParticipantsSearch{Q: p.Query}
		localQuery = ""
	default:
		filter = &tgapi.ChannelParticipantsRecent{}
	}

	resp, err := s.getChannelParticipants(ctx, peer, filter, offset, p.Limit)
	if err != nil {
		return nil, err
	}
	users := usersByID(resp.Users)
	out := []Participant{}
	for _, pc := range resp.Participants {
		id, role, date, ok := channelParticipant(pc)
		if !ok {
			continue
		}
		u, ok := users[id]
		if !ok || !matchesQuery(u, localQuery) {
			continue
		}
		part := newParticipant(u)
		part.Role, part.JoinedAt = role, int64(date)
		out = append(out, part)
	}

	if total == nil {
		total = &resp.Count
	}
	res := &ListParticipantsResult{Source: SourceMembers, Participants: out, Total: total}
	if len(resp.Participants) == p.Limit {
		next := encodeParticipantsCursor(participantsCursor{ChatID: p.ChatID, Source: SourceMembers, Offset: offset + len(resp.Participants)})
		res.NextCursor = &next
	}
	return res, nil
}

// getChannelParticipants calls channels.getParticipants and caches the users.
// A not-modified answer is returned as an empty page.
func (s *Service) getChannelParticipants(ctx context.Context, peer tgapi.InputPeerClass, filter tgapi.ChannelParticipantsFilterClass, offset, limit int) (*tgapi.ChannelsChannelParticipants, error) {
	pc, ok := peer.(*tgapi.InputPeerChannel)
	if !ok {
		return nil, fmt.Errorf("channels.getParticipants: peer %T is not a channel", peer)
	}
	raw, err := s.api.ChannelsGetParticipants(ctx, &tgapi.ChannelsGetParticipantsRequest{
		Channel: &tgapi.InputChannel{ChannelID: pc.ChannelID, AccessHash: pc.AccessHash},
		Filter:  filter,
		Offset:  offset,
		Limit:   limit,
	})
	if err != nil {
		return nil, wrapAccessErr("channels.getParticipants", err)
	}
	resp, ok := raw.(*tgapi.ChannelsChannelParticipants)
	if !ok {
		return &tgapi.ChannelsChannelParticipants{}, nil
	}
	s.cacheAndSave(resp.Users, resp.Chats)
	return resp, nil
}

// channelParticipant returns the user id, role and join date. Banned and
// left entries, and non-user peers, give ok=false.
func channelParticipant(pc tgapi.ChannelParticipantClass) (id int64, role string, date int, ok bool) {
	switch v := pc.(type) {
	case *tgapi.ChannelParticipantCreator:
		return v.UserID, roleCreator, 0, true
	case *tgapi.ChannelParticipantAdmin:
		return v.UserID, roleAdmin, v.Date, true
	case *tgapi.ChannelParticipant:
		return v.UserID, roleMember, v.Date, true
	case *tgapi.ChannelParticipantSelf:
		return v.UserID, roleMember, v.Date, true
	}
	return 0, "", 0, false
}

func groupMembers(p ListParticipantsParams, offset int, list *tgapi.ChatParticipants, users map[int64]*tgapi.User) *ListParticipantsResult {
	all := []Participant{}
	for _, pc := range list.Participants {
		var (
			id   int64
			role string
			date int
		)
		switch v := pc.(type) {
		case *tgapi.ChatParticipantCreator:
			id, role = v.UserID, roleCreator
		case *tgapi.ChatParticipantAdmin:
			id, role, date = v.UserID, roleAdmin, v.Date
		case *tgapi.ChatParticipant:
			id, role, date = v.UserID, roleMember, v.Date
		default:
			continue
		}
		u, ok := users[id]
		if !ok || !matchesQuery(u, p.Query) {
			continue
		}
		if p.Filter == FilterAdmins && role == roleMember {
			continue
		}
		if p.Filter == FilterBots && !u.Bot {
			continue
		}
		part := newParticipant(u)
		part.Role, part.JoinedAt = role, int64(date)
		all = append(all, part)
	}

	total := len(list.Participants)
	res := &ListParticipantsResult{Source: SourceMembers, Participants: []Participant{}, Total: &total}
	if offset < len(all) {
		end := min(offset+p.Limit, len(all))
		res.Participants = all[offset:end]
		if end < len(all) {
			next := encodeParticipantsCursor(participantsCursor{ChatID: p.ChatID, Source: SourceMembers, Offset: end})
			res.NextCursor = &next
		}
	}
	return res
}

func findChannel(chats []tgapi.ChatClass, id int64) *tgapi.Channel {
	for _, c := range chats {
		if ch, ok := c.(*tgapi.Channel); ok && ch.ID == id {
			return ch
		}
	}
	return nil
}

func usersByID(users []tgapi.UserClass) map[int64]*tgapi.User {
	m := make(map[int64]*tgapi.User, len(users))
	for _, uc := range users {
		if u, ok := uc.(*tgapi.User); ok {
			m[u.ID] = u
		}
	}
	return m
}

func newParticipant(u *tgapi.User) Participant {
	return Participant{
		ID:        u.ID,
		FirstName: u.FirstName,
		LastName:  u.LastName,
		Username:  u.Username,
		Bot:       u.Bot,
		Deleted:   u.Deleted,
	}
}

// matchesQuery reports whether q is a case-insensitive substring of the
// user's first name, last name or username. An empty q matches everyone.
func matchesQuery(u *tgapi.User, q string) bool {
	if q == "" {
		return true
	}
	q = strings.ToLower(q)
	for _, f := range []string{u.FirstName, u.LastName, u.Username} {
		if strings.Contains(strings.ToLower(f), q) {
			return true
		}
	}
	return false
}
