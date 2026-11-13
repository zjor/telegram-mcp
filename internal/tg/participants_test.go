package tg

import (
	"context"
	"errors"
	"testing"

	"github.com/gotd/td/bin"
	tgapi "github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
	"github.com/gotd/td/tgmock"
)

const (
	sgID   = int64(100)
	sgHash = int64(1000)
	grpID  = int64(5)
)

func rpcErr(typ string) *tgerr.Error { return &tgerr.Error{Code: 400, Message: typ, Type: typ} }

// sgService returns a service with supergroup sgID cached.
func sgService(t *testing.T) (*Service, *tgmock.Mock) {
	s, mock, _ := newTestService(t)
	s.peers.Put(ChatSupergroup, sgID, sgHash)
	return s, mock
}

func expectFullChannel(t *testing.T, mock *tgmock.Mock, hash int64, full *tgapi.ChannelFull) {
	mock.ExpectFunc(func(b bin.Encoder) {
		t.Helper()
		req, ok := b.(*tgapi.ChannelsGetFullChannelRequest)
		if !ok {
			t.Fatalf("request type %T, want *ChannelsGetFullChannelRequest", b)
		}
		in, ok := req.Channel.(*tgapi.InputChannel)
		if !ok || in.ChannelID != sgID || in.AccessHash != hash {
			t.Errorf("Channel = %#v, want InputChannel{%d, %d}", req.Channel, sgID, hash)
		}
	}).ThenResult(&tgapi.MessagesChatFull{FullChat: full})
}

func visibleChannel(count int) *tgapi.ChannelFull {
	return &tgapi.ChannelFull{ID: sgID, CanViewParticipants: true, ParticipantsCount: count, ChatPhoto: &tgapi.PhotoEmpty{}}
}

// expectParticipants checks the getParticipants request and returns res.
func expectParticipants(t *testing.T, mock *tgmock.Mock, check func(*tgapi.ChannelsGetParticipantsRequest), res *tgapi.ChannelsChannelParticipants) {
	mock.ExpectFunc(func(b bin.Encoder) {
		t.Helper()
		req, ok := b.(*tgapi.ChannelsGetParticipantsRequest)
		if !ok {
			t.Fatalf("request type %T, want *ChannelsGetParticipantsRequest", b)
		}
		check(req)
	}).ThenResult(res)
}

func user(id int64, first, username string) *tgapi.User {
	return &tgapi.User{ID: id, AccessHash: id * 10, FirstName: first, Username: username}
}

func threeMembers() *tgapi.ChannelsChannelParticipants {
	return &tgapi.ChannelsChannelParticipants{
		Count: 3,
		Participants: []tgapi.ChannelParticipantClass{
			&tgapi.ChannelParticipantCreator{UserID: 1},
			&tgapi.ChannelParticipantAdmin{UserID: 2, Date: 200},
			&tgapi.ChannelParticipant{UserID: 3, Date: 300},
		},
		Users: []tgapi.UserClass{user(1, "Ann", "ann_lee"), user(2, "Bob", ""), user(3, "Cid", "")},
	}
}

func list(t *testing.T, s *Service, p ListParticipantsParams) *ListParticipantsResult {
	t.Helper()
	if p.Limit == 0 {
		p.Limit = DefaultLimit
	}
	if p.Filter == "" {
		p.Filter = FilterAll
	}
	res, err := s.ListParticipants(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func ids(ps []Participant) []int64 {
	out := []int64{}
	for _, p := range ps {
		out = append(out, p.ID)
	}
	return out
}

func equalIDs(t *testing.T, got []Participant, want ...int64) {
	t.Helper()
	g := ids(got)
	if len(g) != len(want) {
		t.Fatalf("ids = %v, want %v", g, want)
	}
	for i := range want {
		if g[i] != want[i] {
			t.Fatalf("ids = %v, want %v", g, want)
		}
	}
}

func TestListParticipants_SupergroupRecent(t *testing.T) {
	s, mock := sgService(t)
	expectFullChannel(t, mock, sgHash, visibleChannel(3))
	expectParticipants(t, mock, func(r *tgapi.ChannelsGetParticipantsRequest) {
		if _, ok := r.Filter.(*tgapi.ChannelParticipantsRecent); !ok {
			t.Errorf("Filter = %T, want Recent", r.Filter)
		}
		if r.Offset != 0 || r.Limit != 50 {
			t.Errorf("Offset/Limit = %d/%d, want 0/50", r.Offset, r.Limit)
		}
	}, threeMembers())

	res := list(t, s, ListParticipantsParams{ChatID: sgID, ChatType: ChatSupergroup})
	if res.Source != SourceMembers || res.Total == nil || *res.Total != 3 || res.NextCursor != nil {
		t.Fatalf("res = %+v", res)
	}
	want := []Participant{
		{ID: 1, FirstName: "Ann", Username: "ann_lee", Role: "creator"},
		{ID: 2, FirstName: "Bob", Role: "admin", JoinedAt: 200},
		{ID: 3, FirstName: "Cid", Role: "member", JoinedAt: 300},
	}
	for i := range want {
		if res.Participants[i] != want[i] {
			t.Errorf("participant %d = %+v, want %+v", i, res.Participants[i], want[i])
		}
	}
	if !mock.AllWereMet() {
		t.Fatal("not all expected calls were made")
	}
}

func TestListParticipants_SupergroupSearch(t *testing.T) {
	s, mock := sgService(t)
	expectFullChannel(t, mock, sgHash, visibleChannel(3))
	expectParticipants(t, mock, func(r *tgapi.ChannelsGetParticipantsRequest) {
		f, ok := r.Filter.(*tgapi.ChannelParticipantsSearch)
		if !ok || f.Q != "ann" {
			t.Errorf("Filter = %#v, want Search{ann}", r.Filter)
		}
	}, threeMembers())
	list(t, s, ListParticipantsParams{ChatID: sgID, ChatType: ChatSupergroup, Query: "ann"})
}

func TestListParticipants_SupergroupAdminsQuery(t *testing.T) {
	s, mock := sgService(t)
	expectFullChannel(t, mock, sgHash, visibleChannel(3))
	expectParticipants(t, mock, func(r *tgapi.ChannelsGetParticipantsRequest) {
		if _, ok := r.Filter.(*tgapi.ChannelParticipantsAdmins); !ok {
			t.Errorf("Filter = %T, want Admins", r.Filter)
		}
	}, threeMembers())
	res := list(t, s, ListParticipantsParams{ChatID: sgID, ChatType: ChatSupergroup, Filter: FilterAdmins, Query: "bo"})
	equalIDs(t, res.Participants, 2)
}

func TestListParticipants_SupergroupBots(t *testing.T) {
	s, mock := sgService(t)
	expectFullChannel(t, mock, sgHash, visibleChannel(3))
	expectParticipants(t, mock, func(r *tgapi.ChannelsGetParticipantsRequest) {
		if _, ok := r.Filter.(*tgapi.ChannelParticipantsBots); !ok {
			t.Errorf("Filter = %T, want Bots", r.Filter)
		}
	}, threeMembers())
	list(t, s, ListParticipantsParams{ChatID: sgID, ChatType: ChatSupergroup, Filter: FilterBots})
}

func TestListParticipants_SupergroupPaging(t *testing.T) {
	s, mock := sgService(t)
	page1 := threeMembers()
	page1.Participants = page1.Participants[:2]
	expectFullChannel(t, mock, sgHash, visibleChannel(3))
	expectParticipants(t, mock, func(r *tgapi.ChannelsGetParticipantsRequest) {
		if r.Offset != 0 || r.Limit != 2 {
			t.Errorf("Offset/Limit = %d/%d, want 0/2", r.Offset, r.Limit)
		}
	}, page1)

	res := list(t, s, ListParticipantsParams{ChatID: sgID, ChatType: ChatSupergroup, Limit: 2})
	if res.NextCursor == nil {
		t.Fatal("NextCursor = nil, want set")
	}
	c, err := decodeParticipantsCursor(*res.NextCursor, sgID)
	if err != nil || c.Source != SourceMembers || c.Offset != 2 {
		t.Fatalf("cursor = %+v, %v", c, err)
	}

	page2 := threeMembers()
	page2.Participants = page2.Participants[2:]
	expectFullChannel(t, mock, sgHash, visibleChannel(3))
	expectParticipants(t, mock, func(r *tgapi.ChannelsGetParticipantsRequest) {
		if r.Offset != 2 {
			t.Errorf("Offset = %d, want 2", r.Offset)
		}
	}, page2)
	res = list(t, s, ListParticipantsParams{ChatID: sgID, ChatType: ChatSupergroup, Limit: 2, Cursor: *res.NextCursor})
	equalIDs(t, res.Participants, 3)
	if res.NextCursor != nil {
		t.Fatalf("NextCursor = %v, want nil", *res.NextCursor)
	}
}

func TestListParticipants_SkipsBannedLeft(t *testing.T) {
	s, mock := sgService(t)
	expectFullChannel(t, mock, sgHash, visibleChannel(3))
	expectParticipants(t, mock, func(*tgapi.ChannelsGetParticipantsRequest) {}, &tgapi.ChannelsChannelParticipants{
		Count: 3,
		Participants: []tgapi.ChannelParticipantClass{
			&tgapi.ChannelParticipantBanned{Peer: &tgapi.PeerUser{UserID: 1}},
			&tgapi.ChannelParticipantLeft{Peer: &tgapi.PeerUser{UserID: 2}},
		},
		Users: []tgapi.UserClass{user(1, "Ann", ""), user(2, "Bob", "")},
	})
	res := list(t, s, ListParticipantsParams{ChatID: sgID, ChatType: ChatSupergroup, Limit: 2})
	if res.Participants == nil || len(res.Participants) != 0 {
		t.Fatalf("Participants = %#v, want empty non-nil", res.Participants)
	}
	if res.NextCursor == nil {
		t.Fatal("NextCursor = nil, want set: Telegram returned a full page")
	}
}

func TestListParticipants_MissingUserSkipped(t *testing.T) {
	s, mock := sgService(t)
	page := threeMembers()
	page.Participants = append(page.Participants, &tgapi.ChannelParticipant{UserID: 99})
	page.Users = append(page.Users, &tgapi.UserEmpty{ID: 98})
	page.Participants = append(page.Participants, &tgapi.ChannelParticipant{UserID: 98})
	expectFullChannel(t, mock, sgHash, visibleChannel(5))
	expectParticipants(t, mock, func(*tgapi.ChannelsGetParticipantsRequest) {}, page)
	res := list(t, s, ListParticipantsParams{ChatID: sgID, ChatType: ChatSupergroup})
	equalIDs(t, res.Participants, 1, 2, 3)
}

func TestListParticipants_CachesUsers(t *testing.T) {
	s, mock := sgService(t)
	expectFullChannel(t, mock, sgHash, visibleChannel(3))
	expectParticipants(t, mock, func(*tgapi.ChannelsGetParticipantsRequest) {}, threeMembers())
	list(t, s, ListParticipantsParams{ChatID: sgID, ChatType: ChatSupergroup})
	for _, id := range []int64{1, 2, 3} {
		if _, ok := s.peers.Get(ChatPrivate, id); !ok {
			t.Errorf("user %d not in peer cache", id)
		}
	}
}

func basicGroupFull() *tgapi.MessagesChatFull {
	return &tgapi.MessagesChatFull{
		FullChat: &tgapi.ChatFull{ID: grpID, Participants: &tgapi.ChatParticipants{
			ChatID: grpID,
			Participants: []tgapi.ChatParticipantClass{
				&tgapi.ChatParticipantCreator{UserID: 1},
				&tgapi.ChatParticipantAdmin{UserID: 2, Date: 200},
				&tgapi.ChatParticipant{UserID: 3, Date: 300},
			},
		}},
		Users: []tgapi.UserClass{user(1, "Ann", "ann_lee"), user(2, "Bob", ""), &tgapi.User{ID: 3, AccessHash: 30, FirstName: "Bot", Bot: true}},
	}
}

func expectFullChat(t *testing.T, mock *tgmock.Mock, res *tgapi.MessagesChatFull) {
	mock.ExpectFunc(func(b bin.Encoder) {
		t.Helper()
		req, ok := b.(*tgapi.MessagesGetFullChatRequest)
		if !ok || req.ChatID != grpID {
			t.Fatalf("request = %#v, want MessagesGetFullChatRequest{%d}", b, grpID)
		}
	}).ThenResult(res)
}

func grpService(t *testing.T) (*Service, *tgmock.Mock) {
	s, mock, _ := newTestService(t)
	s.peers.Put(ChatGroup, grpID, 0)
	return s, mock
}

func TestListParticipants_BasicGroup(t *testing.T) {
	s, mock := grpService(t)
	expectFullChat(t, mock, basicGroupFull())
	res := list(t, s, ListParticipantsParams{ChatID: grpID, ChatType: ChatGroup, Limit: 2})
	if res.Source != SourceMembers || res.Total == nil || *res.Total != 3 {
		t.Fatalf("res = %+v", res)
	}
	if res.Participants[0].Role != "creator" || res.Participants[1].Role != "admin" || res.Participants[1].JoinedAt != 200 {
		t.Fatalf("participants = %+v", res.Participants)
	}
	equalIDs(t, res.Participants, 1, 2)
	if res.NextCursor == nil {
		t.Fatal("NextCursor = nil, want set")
	}

	expectFullChat(t, mock, basicGroupFull())
	res = list(t, s, ListParticipantsParams{ChatID: grpID, ChatType: ChatGroup, Limit: 2, Cursor: *res.NextCursor})
	equalIDs(t, res.Participants, 3)
	if res.Participants[0].Role != "member" || !res.Participants[0].Bot || res.NextCursor != nil {
		t.Fatalf("res = %+v", res)
	}
}

func TestListParticipants_BasicGroupFilters(t *testing.T) {
	for _, tc := range []struct {
		filter ParticipantFilter
		query  string
		want   []int64
	}{
		{FilterAdmins, "", []int64{1, 2}},
		{FilterBots, "", []int64{3}},
		{FilterAll, "ANN", []int64{1}},
	} {
		s, mock := grpService(t)
		expectFullChat(t, mock, basicGroupFull())
		res := list(t, s, ListParticipantsParams{ChatID: grpID, ChatType: ChatGroup, Filter: tc.filter, Query: tc.query})
		equalIDs(t, res.Participants, tc.want...)
	}
}

func TestListParticipants_NotAMember(t *testing.T) {
	s, mock := sgService(t)
	mock.ExpectFunc(func(bin.Encoder) {}).ThenRPCErr(rpcErr("CHANNEL_PRIVATE"))
	_, err := s.ListParticipants(context.Background(), ListParticipantsParams{ChatID: sgID, ChatType: ChatSupergroup, Filter: FilterAll, Limit: 50})
	if !errors.Is(err, ErrNotAMember) {
		t.Fatalf("err = %v, want ErrNotAMember", err)
	}
}

func TestListParticipants_StaleHashRetry(t *testing.T) {
	s, mock := sgService(t)
	mock.ExpectFunc(func(bin.Encoder) {}).ThenRPCErr(rpcErr("CHANNEL_INVALID"))
	mock.ExpectFunc(func(b bin.Encoder) { dialogsRequest(t, b) }).ThenResult(&tgapi.MessagesDialogs{
		Dialogs: []tgapi.DialogClass{&tgapi.Dialog{Peer: &tgapi.PeerChannel{ChannelID: sgID}}},
		Chats:   []tgapi.ChatClass{&tgapi.Channel{ID: sgID, AccessHash: 2000, Megagroup: true, Title: "G", Photo: &tgapi.ChatPhotoEmpty{}}},
	})
	expectFullChannel(t, mock, 2000, visibleChannel(3))
	expectParticipants(t, mock, func(*tgapi.ChannelsGetParticipantsRequest) {}, threeMembers())
	list(t, s, ListParticipantsParams{ChatID: sgID, ChatType: ChatSupergroup})
	if !mock.AllWereMet() {
		t.Fatal("not all expected calls were made")
	}
}

func TestListParticipants_CursorWrongChatNoCalls(t *testing.T) {
	s, _ := sgService(t)
	c := encodeParticipantsCursor(participantsCursor{ChatID: 1, Source: SourceMembers, Offset: 5})
	_, err := s.ListParticipants(context.Background(), ListParticipantsParams{ChatID: sgID, ChatType: ChatSupergroup, Filter: FilterAll, Limit: 50, Cursor: c})
	if !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("err = %v, want ErrInvalidCursor", err)
	}
}

func TestListParticipants_CursorSourceChanged(t *testing.T) {
	s, mock := sgService(t)
	c := encodeParticipantsCursor(participantsCursor{ChatID: sgID, Source: SourceRecentSenders, BeforeID: 500})
	expectFullChannel(t, mock, sgHash, visibleChannel(3))
	expectParticipants(t, mock, func(r *tgapi.ChannelsGetParticipantsRequest) {
		if r.Offset != 0 {
			t.Errorf("Offset = %d, want 0", r.Offset)
		}
	}, threeMembers())
	list(t, s, ListParticipantsParams{ChatID: sgID, ChatType: ChatSupergroup, Cursor: c})
}

func TestMatchesQuery(t *testing.T) {
	u := &tgapi.User{FirstName: "Ann", LastName: "Lee", Username: "ann_lee"}
	for q, want := range map[string]bool{"aNN": true, "LEE_": false, "_lee": true, "": true, "zz": false} {
		if got := matchesQuery(u, q); got != want {
			t.Errorf("matchesQuery(%q) = %v, want %v", q, got, want)
		}
	}
}
