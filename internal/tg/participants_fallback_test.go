package tg

import (
	"context"
	"errors"
	"testing"

	"github.com/gotd/td/bin"
	tgapi "github.com/gotd/td/tg"
	"github.com/gotd/td/tgmock"
)

func hiddenChannel() *tgapi.ChannelFull {
	return &tgapi.ChannelFull{ID: sgID, CanViewParticipants: true, ParticipantsHidden: true, ParticipantsCount: 5000, ChatPhoto: &tgapi.PhotoEmpty{}}
}

func fromMsg(id, date int, from int64) *tgapi.Message {
	return textMsg(id, date, &tgapi.PeerUser{UserID: from}, "x")
}

// expectHistory checks OffsetID and Limit 100 and returns a slice page.
func expectHistory(t *testing.T, mock *tgmock.Mock, offsetID int, msgs []tgapi.MessageClass, users []tgapi.UserClass) {
	mock.ExpectFunc(func(b bin.Encoder) {
		t.Helper()
		assertHistoryRequest(t, historyRequest(t, b), offsetID, 100)
	}).ThenResult(&tgapi.MessagesChannelMessages{Messages: msgs, Users: users, Count: 100000})
}

func people() []tgapi.UserClass {
	return []tgapi.UserClass{
		user(7, "Ann", "ann_lee"), user(8, "Bob", ""), user(9, "Cid", ""),
		&tgapi.User{ID: 10, AccessHash: 100, FirstName: "Robo", Bot: true},
	}
}

func fullPage(firstID int, from int64) []tgapi.MessageClass {
	out := make([]tgapi.MessageClass, 100)
	for i := range out {
		out[i] = fromMsg(firstID-i, 1000-i, from)
	}
	return out
}

func TestFallback_HiddenSupergroup(t *testing.T) {
	s, mock := sgService(t)
	expectFullChannel(t, mock, sgHash, hiddenChannel())
	expectHistory(t, mock, 0, []tgapi.MessageClass{fromMsg(3, 300, 7)}, people())
	res := list(t, s, ListParticipantsParams{ChatID: sgID, ChatType: ChatSupergroup})
	if res.Source != SourceRecentSenders || res.Total != nil || res.NextCursor != nil {
		t.Fatalf("res = %+v", res)
	}
	if res.Participants[0] != (Participant{ID: 7, FirstName: "Ann", Username: "ann_lee", LastMessageDate: 300}) {
		t.Fatalf("participant = %+v", res.Participants[0])
	}
}

func TestFallback_CannotViewParticipants(t *testing.T) {
	s, mock := sgService(t)
	full := hiddenChannel()
	full.ParticipantsHidden, full.CanViewParticipants = false, false
	expectFullChannel(t, mock, sgHash, full)
	expectHistory(t, mock, 0, []tgapi.MessageClass{fromMsg(3, 300, 7)}, people())
	if res := list(t, s, ListParticipantsParams{ChatID: sgID, ChatType: ChatSupergroup}); res.Source != SourceRecentSenders {
		t.Fatalf("Source = %q", res.Source)
	}
}

func TestFallback_BasicGroupForbidden(t *testing.T) {
	s, mock := grpService(t)
	expectFullChat(t, mock, &tgapi.MessagesChatFull{FullChat: &tgapi.ChatFull{ID: grpID, Participants: &tgapi.ChatParticipantsForbidden{ChatID: grpID}}})
	mock.ExpectFunc(func(b bin.Encoder) { historyRequest(t, b) }).
		ThenResult(&tgapi.MessagesMessages{Messages: []tgapi.MessageClass{fromMsg(3, 300, 8)}, Users: people()})
	res := list(t, s, ListParticipantsParams{ChatID: grpID, ChatType: ChatGroup})
	if res.Source != SourceRecentSenders || res.NextCursor != nil {
		t.Fatalf("res = %+v", res)
	}
	equalIDs(t, res.Participants, 8)
}

func TestFallback_DedupNewestFirst(t *testing.T) {
	s, mock := sgService(t)
	expectFullChannel(t, mock, sgHash, hiddenChannel())
	expectHistory(t, mock, 0, []tgapi.MessageClass{fromMsg(3, 300, 7), fromMsg(2, 200, 8), fromMsg(1, 100, 7)}, people())
	res := list(t, s, ListParticipantsParams{ChatID: sgID, ChatType: ChatSupergroup})
	equalIDs(t, res.Participants, 7, 8)
	if res.Participants[0].LastMessageDate != 300 {
		t.Fatalf("LastMessageDate = %d, want 300", res.Participants[0].LastMessageDate)
	}
}

func TestFallback_SkipsNonUserSenders(t *testing.T) {
	s, mock := sgService(t)
	expectFullChannel(t, mock, sgHash, hiddenChannel())
	expectHistory(t, mock, 0, []tgapi.MessageClass{
		textMsg(4, 400, nil, "no from"),
		textMsg(3, 300, &tgapi.PeerChannel{ChannelID: sgID}, "anon admin"),
		&tgapi.MessageEmpty{ID: 2},
		fromMsg(1, 100, 8),
	}, people())
	res := list(t, s, ListParticipantsParams{ChatID: sgID, ChatType: ChatSupergroup})
	equalIDs(t, res.Participants, 8)
}

func TestFallback_ServiceMessageCounts(t *testing.T) {
	s, mock := sgService(t)
	expectFullChannel(t, mock, sgHash, hiddenChannel())
	svc := &tgapi.MessageService{ID: 5, Date: 500, PeerID: &tgapi.PeerChannel{ChannelID: sgID}, Action: &tgapi.MessageActionChatJoinedByLink{}}
	svc.SetFromID(&tgapi.PeerUser{UserID: 9})
	expectHistory(t, mock, 0, []tgapi.MessageClass{svc}, people())
	res := list(t, s, ListParticipantsParams{ChatID: sgID, ChatType: ChatSupergroup})
	equalIDs(t, res.Participants, 9)
}

func TestFallback_StopsAtLimit(t *testing.T) {
	s, mock := sgService(t)
	expectFullChannel(t, mock, sgHash, hiddenChannel())
	expectHistory(t, mock, 0, []tgapi.MessageClass{fromMsg(30, 300, 7), fromMsg(20, 200, 8), fromMsg(10, 100, 9)}, people())
	res := list(t, s, ListParticipantsParams{ChatID: sgID, ChatType: ChatSupergroup, Limit: 2})
	equalIDs(t, res.Participants, 7, 8)
	if res.NextCursor == nil {
		t.Fatal("NextCursor = nil, want set")
	}
	c, err := decodeParticipantsCursor(*res.NextCursor, sgID)
	if err != nil || c.Source != SourceRecentSenders || c.BeforeID != 20 {
		t.Fatalf("cursor = %+v, %v; want BeforeID 20", c, err)
	}

	// The next call continues below message 20.
	expectFullChannel(t, mock, sgHash, hiddenChannel())
	expectHistory(t, mock, 20, []tgapi.MessageClass{fromMsg(10, 100, 9)}, people())
	res = list(t, s, ListParticipantsParams{ChatID: sgID, ChatType: ChatSupergroup, Limit: 2, Cursor: *res.NextCursor})
	equalIDs(t, res.Participants, 9)
}

func TestFallback_StopsAt1000(t *testing.T) {
	s, mock := sgService(t)
	expectFullChannel(t, mock, sgHash, hiddenChannel())
	offset := 0
	for i := 0; i < 10; i++ {
		first := 5000 - i*100
		expectHistory(t, mock, offset, fullPage(first, 7), people())
		offset = first - 99
	}
	res := list(t, s, ListParticipantsParams{ChatID: sgID, ChatType: ChatSupergroup})
	equalIDs(t, res.Participants, 7)
	if !mock.AllWereMet() {
		t.Fatal("not all 10 history calls were made")
	}
	c, err := decodeParticipantsCursor(*res.NextCursor, sgID)
	if err != nil || c.BeforeID != offset {
		t.Fatalf("cursor = %+v, %v; want BeforeID %d", c, err, offset)
	}
}

func TestFallback_HistoryEnds(t *testing.T) {
	s, mock := sgService(t)
	expectFullChannel(t, mock, sgHash, hiddenChannel())
	expectHistory(t, mock, 0, []tgapi.MessageClass{fromMsg(3, 300, 7), fromMsg(2, 200, 8), fromMsg(1, 100, 9)}, people())
	if res := list(t, s, ListParticipantsParams{ChatID: sgID, ChatType: ChatSupergroup}); res.NextCursor != nil {
		t.Fatalf("NextCursor = %v, want nil", *res.NextCursor)
	}
}

func TestFallback_BotsAndQuery(t *testing.T) {
	msgs := []tgapi.MessageClass{fromMsg(4, 400, 10), fromMsg(3, 300, 7), fromMsg(2, 200, 8)}
	for _, tc := range []struct {
		filter ParticipantFilter
		query  string
		want   int64
	}{{FilterBots, "", 10}, {FilterAll, "aNN", 7}} {
		s, mock := sgService(t)
		expectFullChannel(t, mock, sgHash, hiddenChannel())
		expectHistory(t, mock, 0, msgs, people())
		res := list(t, s, ListParticipantsParams{ChatID: sgID, ChatType: ChatSupergroup, Filter: tc.filter, Query: tc.query})
		equalIDs(t, res.Participants, tc.want)
	}
}

func TestFallback_AdminsVisible(t *testing.T) {
	s, mock := sgService(t)
	expectFullChannel(t, mock, sgHash, hiddenChannel())
	page := threeMembers()
	page.Participants, page.Count = page.Participants[:2], 2
	expectParticipants(t, mock, func(r *tgapi.ChannelsGetParticipantsRequest) {
		if _, ok := r.Filter.(*tgapi.ChannelParticipantsAdmins); !ok {
			t.Errorf("Filter = %T, want Admins", r.Filter)
		}
	}, page)
	res := list(t, s, ListParticipantsParams{ChatID: sgID, ChatType: ChatSupergroup, Filter: FilterAdmins})
	if res.Source != SourceMembers || res.Total == nil || *res.Total != 2 {
		t.Fatalf("res = %+v", res)
	}
	equalIDs(t, res.Participants, 1, 2)
}

func TestFallback_AdminsHidden(t *testing.T) {
	for _, typ := range []string{"CHAT_ADMIN_REQUIRED", "CHANNEL_PRIVATE"} {
		s, mock := sgService(t)
		expectFullChannel(t, mock, sgHash, hiddenChannel())
		mock.ExpectFunc(func(bin.Encoder) {}).ThenRPCErr(rpcErr(typ))
		_, err := s.ListParticipants(context.Background(), ListParticipantsParams{ChatID: sgID, ChatType: ChatSupergroup, Filter: FilterAdmins, Limit: 50})
		if !errors.Is(err, ErrAdminsHidden) {
			t.Fatalf("%s: err = %v, want ErrAdminsHidden", typ, err)
		}
	}
}

func TestFallback_AdminsBasicGroup(t *testing.T) {
	s, mock := grpService(t)
	expectFullChat(t, mock, &tgapi.MessagesChatFull{FullChat: &tgapi.ChatFull{ID: grpID, Participants: &tgapi.ChatParticipantsForbidden{ChatID: grpID}}})
	_, err := s.ListParticipants(context.Background(), ListParticipantsParams{ChatID: grpID, ChatType: ChatGroup, Filter: FilterAdmins, Limit: 50})
	if !errors.Is(err, ErrAdminsHidden) {
		t.Fatalf("err = %v, want ErrAdminsHidden", err)
	}
	if !mock.AllWereMet() {
		t.Fatal("unexpected state")
	}
}

func expectFullChannelWith(t *testing.T, mock *tgmock.Mock, full *tgapi.ChannelFull, ch *tgapi.Channel) {
	ch.ID, ch.AccessHash, ch.Photo = sgID, sgHash, &tgapi.ChatPhotoEmpty{}
	mock.ExpectFunc(func(b bin.Encoder) {
		t.Helper()
		if _, ok := b.(*tgapi.ChannelsGetFullChannelRequest); !ok {
			t.Fatalf("request type %T, want *ChannelsGetFullChannelRequest", b)
		}
	}).ThenResult(&tgapi.MessagesChatFull{FullChat: full, Chats: []tgapi.ChatClass{ch}})
}

func TestListParticipants_AdminOfHiddenGroupGetsMembers(t *testing.T) {
	admin := &tgapi.Channel{Megagroup: true}
	admin.SetAdminRights(tgapi.ChatAdminRights{BanUsers: true})
	for name, ch := range map[string]*tgapi.Channel{"creator": {Megagroup: true, Creator: true}, "admin": admin} {
		s, mock := sgService(t)
		expectFullChannelWith(t, mock, hiddenChannel(), ch)
		expectParticipants(t, mock, func(r *tgapi.ChannelsGetParticipantsRequest) {
			if _, ok := r.Filter.(*tgapi.ChannelParticipantsRecent); !ok {
				t.Errorf("%s: Filter = %T, want Recent", name, r.Filter)
			}
		}, threeMembers())
		if res := list(t, s, ListParticipantsParams{ChatID: sgID, ChatType: ChatSupergroup}); res.Source != SourceMembers {
			t.Fatalf("%s: Source = %q, want members", name, res.Source)
		}
	}
}

func TestListParticipants_BroadcastRejected(t *testing.T) {
	s, mock := sgService(t)
	full := hiddenChannel()
	full.CanViewParticipants, full.ParticipantsHidden = false, false
	expectFullChannelWith(t, mock, full, &tgapi.Channel{Broadcast: true})
	_, err := s.ListParticipants(context.Background(), ListParticipantsParams{ChatID: sgID, ChatType: ChatSupergroup, Filter: FilterAll, Limit: 50})
	if !errors.Is(err, ErrNotAGroup) || err.Error() != "list_participants supports group and supergroup only" {
		t.Fatalf("err = %v, want ErrNotAGroup", err)
	}
}
