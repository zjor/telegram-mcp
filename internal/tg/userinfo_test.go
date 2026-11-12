package tg

import (
	"encoding/json"
	"reflect"
	"testing"

	tgapi "github.com/gotd/td/tg"
)

func TestNewUserInfo_FullMapping(t *testing.T) {
	u := &tgapi.User{
		ID: 7, FirstName: "Ann", LastName: "Lee", Username: "ann",
		Phone: "4912345", LangCode: "de", Contact: true, MutualContact: true,
		CloseFriend: true, Premium: true, Restricted: true,
		Photo: &tgapi.UserProfilePhoto{PhotoID: 1},
		Usernames: []tgapi.Username{
			{Username: "ann", Active: true, Editable: true},
			{Username: "annie", Active: true},
			{Username: "old"},
		},
		Status:            &tgapi.UserStatusOffline{WasOnline: 1700000000},
		RestrictionReason: []tgapi.RestrictionReason{{Platform: "ios", Reason: "porn", Text: "x"}},
	}
	f := &tgapi.UserFull{ID: 7, About: "bio", Blocked: true, CommonChatsCount: 3}
	f.SetPersonalChannelID(55)
	f.SetNote(tgapi.TextWithEntities{Text: "met in Berlin"})
	f.SetBirthday(tgapi.Birthday{Day: 2, Month: 3})

	got := newUserInfo(u, f)
	want := UserInfo{
		ID: 7, FirstName: "Ann", LastName: "Lee", Username: "ann",
		Usernames: []string{"annie"}, Phone: "4912345", Bio: "bio",
		Birthday: &Birthday{Day: 2, Month: 3}, Note: "met in Berlin", LangCode: "de",
		Status: "offline", LastSeen: 1700000000,
		Contact: true, MutualContact: true, CloseFriend: true, Blocked: true, CommonChatsCount: 3,
		Premium: true, Restricted: true, RestrictionReasons: []string{"ios: porn: x"},
		HasPhoto: true, PersonalChannelID: 55,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
}

func TestNewUserInfo_HiddenFieldsOmittedInJSON(t *testing.T) {
	info := newUserInfo(&tgapi.User{ID: 1, FirstName: "X"}, &tgapi.UserFull{ID: 1})
	data, err := json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"phone", "birthday", "bio", "last_seen", "business", "bot", "premium", "usernames", "note"} {
		if _, ok := m[k]; ok {
			t.Errorf("key %q present, want omitted: %s", k, data)
		}
	}
	for _, k := range []string{"contact", "mutual_contact", "close_friend", "blocked", "common_chats_count", "has_photo"} {
		if _, ok := m[k]; !ok {
			t.Errorf("key %q missing: %s", k, data)
		}
	}
	if m["status"] != "unknown" {
		t.Errorf("status = %v, want unknown", m["status"])
	}
}

func TestNewUserInfo_Status(t *testing.T) {
	tests := []struct {
		status   tgapi.UserStatusClass
		want     string
		lastSeen int64
	}{
		{&tgapi.UserStatusOnline{Expires: 9}, "online", 0},
		{&tgapi.UserStatusOffline{WasOnline: 5}, "offline", 5},
		{&tgapi.UserStatusRecently{}, "recently", 0},
		{&tgapi.UserStatusLastWeek{}, "last_week", 0},
		{&tgapi.UserStatusLastMonth{}, "last_month", 0},
		{&tgapi.UserStatusEmpty{}, "unknown", 0},
		{nil, "unknown", 0},
	}
	for _, tt := range tests {
		got := newUserInfo(&tgapi.User{ID: 1, Status: tt.status}, nil)
		if got.Status != tt.want || got.LastSeen != tt.lastSeen {
			t.Errorf("%T: status %q last_seen %d, want %q %d", tt.status, got.Status, got.LastSeen, tt.want, tt.lastSeen)
		}
	}
}

func TestNewUserInfo_CollectibleUsernameOnly(t *testing.T) {
	u := &tgapi.User{ID: 1, Usernames: []tgapi.Username{
		{Username: "u1"},
		{Username: "u2", Active: true},
		{Username: "u3", Active: true},
	}}
	got := newUserInfo(u, nil)
	if got.Username != "u2" || !reflect.DeepEqual(got.Usernames, []string{"u3"}) {
		t.Fatalf("username %q usernames %v, want u2 [u3]", got.Username, got.Usernames)
	}
}

func TestNewUserInfo_DeletedAccount(t *testing.T) {
	got := newUserInfo(&tgapi.User{ID: 9, Deleted: true}, nil)
	if !got.Deleted || got.Status != "unknown" || got.FirstName != "" || got.ID != 9 {
		t.Fatalf("got %+v", got)
	}
}

func TestNewUserInfo_Business(t *testing.T) {
	f := &tgapi.UserFull{ID: 1}
	f.SetBusinessLocation(tgapi.BusinessLocation{Address: "Main 1"})
	f.SetBusinessIntro(tgapi.BusinessIntro{Title: "Hi", Description: "We fix bikes"})
	f.SetBusinessWorkHours(tgapi.BusinessWorkHours{TimezoneID: "Europe/Berlin", WeeklyOpen: []tgapi.BusinessWeeklyOpen{
		{StartMinute: 540, EndMinute: 1080},
		{StartMinute: 4*1440 + 1320, EndMinute: 5*1440 + 120},
	}})
	got := newUserInfo(&tgapi.User{ID: 1}, f).Business
	want := &Business{
		Address: "Main 1",
		Intro:   "Hi\nWe fix bikes",
		WorkHours: &WorkHours{
			Timezone: "Europe/Berlin",
			Open:     []string{"Mon 09:00-18:00", "Fri 22:00-02:00"},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}

	empty := &tgapi.UserFull{ID: 1}
	empty.SetBusinessWorkHours(tgapi.BusinessWorkHours{TimezoneID: "X"})
	if b := newUserInfo(&tgapi.User{ID: 1}, empty).Business; b != nil {
		t.Fatalf("empty business = %+v, want nil", b)
	}

	titleOnly := &tgapi.UserFull{ID: 1}
	titleOnly.SetBusinessIntro(tgapi.BusinessIntro{Title: "Hi"})
	if b := newUserInfo(&tgapi.User{ID: 1}, titleOnly).Business; b == nil || b.Intro != "Hi" || b.WorkHours != nil {
		t.Fatalf("title-only business = %+v, want Intro Hi", b)
	}
}

func TestNewUserInfo_BotDescription(t *testing.T) {
	f := &tgapi.UserFull{ID: 1}
	f.SetBotInfo(tgapi.BotInfo{Description: "I am a bot"})
	got := newUserInfo(&tgapi.User{ID: 1, Bot: true}, f)
	if !got.Bot || got.BotDescription != "I am a bot" {
		t.Fatalf("got bot %v desc %q", got.Bot, got.BotDescription)
	}
}
