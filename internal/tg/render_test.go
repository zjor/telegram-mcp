package tg

import (
	"testing"

	tgapi "github.com/gotd/td/tg"
)

func docMsg(text string, attrs ...tgapi.DocumentAttributeClass) *tgapi.Message {
	return &tgapi.Message{
		Message: text,
		Media:   &tgapi.MessageMediaDocument{Document: &tgapi.Document{Attributes: attrs}},
	}
}

func TestRenderText(t *testing.T) {
	tests := []struct {
		name string
		m    *tgapi.Message
		want string
	}{
		{"plain", &tgapi.Message{Message: "hello"}, "hello"},
		{"photo", &tgapi.Message{Media: &tgapi.MessageMediaPhoto{}}, "[photo]"},
		{"photo caption", &tgapi.Message{Message: "cap", Media: &tgapi.MessageMediaPhoto{}}, "[photo] cap"},
		{"sticker", docMsg("", &tgapi.DocumentAttributeSticker{}), "[sticker]"},
		{"voice", docMsg("", &tgapi.DocumentAttributeAudio{Voice: true}), "[voice]"},
		{"audio", docMsg("", &tgapi.DocumentAttributeAudio{}), "[audio]"},
		{"video note", docMsg("", &tgapi.DocumentAttributeVideo{RoundMessage: true}), "[video_note]"},
		{"video", docMsg("", &tgapi.DocumentAttributeVideo{}), "[video]"},
		{"gif", docMsg("", &tgapi.DocumentAttributeAnimated{}), "[gif]"},
		{"filename", docMsg("", &tgapi.DocumentAttributeFilename{FileName: "a.pdf"}), "[document a.pdf]"},
		{"document", docMsg(""), "[document]"},
		{"geo", &tgapi.Message{Media: &tgapi.MessageMediaGeo{}}, "[location]"},
		{"geo live", &tgapi.Message{Media: &tgapi.MessageMediaGeoLive{}}, "[location]"},
		{"contact", &tgapi.Message{Media: &tgapi.MessageMediaContact{}}, "[contact]"},
		{"poll", &tgapi.Message{Media: &tgapi.MessageMediaPoll{}}, "[poll]"},
		{"webpage", &tgapi.Message{Message: "see https://x", Media: &tgapi.MessageMediaWebPage{}}, "see https://x"},
		{"unsupported", &tgapi.Message{Media: &tgapi.MessageMediaUnsupported{}}, "[media]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RenderText(tt.m); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRenderService(t *testing.T) {
	tests := []struct {
		name   string
		action tgapi.MessageActionClass
		want   string
	}{
		{"pin", &tgapi.MessageActionPinMessage{}, "[service: message pinned]"},
		{"title", &tgapi.MessageActionChatEditTitle{Title: "New"}, "[service: title changed to New]"},
		{"add", &tgapi.MessageActionChatAddUser{}, "[service: user added]"},
		{"delete", &tgapi.MessageActionChatDeleteUser{}, "[service: user removed]"},
		{"call", &tgapi.MessageActionPhoneCall{}, "[service: call]"},
		{"joined", &tgapi.MessageActionChatJoinedByLink{}, "[service: user joined]"},
		{"other", &tgapi.MessageActionChatCreate{}, "[service]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RenderService(&tgapi.MessageService{Action: tt.action}); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestUserName(t *testing.T) {
	tests := []struct {
		u    *tgapi.User
		want string
	}{
		{&tgapi.User{FirstName: "A", LastName: "B"}, "A B"},
		{&tgapi.User{FirstName: "A"}, "A"},
		{&tgapi.User{Deleted: true}, "Deleted Account"},
		{&tgapi.User{}, ""},
	}
	for _, tt := range tests {
		if got := UserName(tt.u); got != tt.want {
			t.Errorf("got %q, want %q", got, tt.want)
		}
	}
}

func TestNames_PeerName(t *testing.T) {
	n := NewNames(
		[]tgapi.UserClass{&tgapi.User{ID: 1, FirstName: "Ann"}, &tgapi.UserEmpty{ID: 2}},
		[]tgapi.ChatClass{
			&tgapi.Chat{ID: 10, Title: "Grp"},
			&tgapi.Channel{ID: 20, Title: "Chan"},
			&tgapi.ChatForbidden{ID: 11, Title: "FGrp"},
			&tgapi.ChannelForbidden{ID: 21, Title: "FChan"},
		},
	)
	tests := []struct {
		name string
		p    tgapi.PeerClass
		want string
	}{
		{"user", &tgapi.PeerUser{UserID: 1}, "Ann"},
		{"user empty", &tgapi.PeerUser{UserID: 2}, "Deleted Account"},
		{"user absent", &tgapi.PeerUser{UserID: 3}, "Deleted Account"},
		{"chat", &tgapi.PeerChat{ChatID: 10}, "Grp"},
		{"channel", &tgapi.PeerChannel{ChannelID: 20}, "Chan"},
		{"chat forbidden", &tgapi.PeerChat{ChatID: 11}, "FGrp"},
		{"channel forbidden", &tgapi.PeerChannel{ChannelID: 21}, "FChan"},
		{"chat absent", &tgapi.PeerChat{ChatID: 99}, ""},
		{"channel absent", &tgapi.PeerChannel{ChannelID: 99}, ""},
		{"nil", nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := n.PeerName(tt.p); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRenderMessage(t *testing.T) {
	names := NewNames([]tgapi.UserClass{&tgapi.User{ID: 7, FirstName: "Ann"}}, nil)

	if _, ok := RenderMessage(&tgapi.MessageEmpty{}, names, "Chan"); ok {
		t.Error("MessageEmpty must give ok == false")
	}

	tests := []struct {
		name     string
		m        tgapi.MessageClass
		wantFrom string
		wantText string
	}{
		{"out", &tgapi.Message{Out: true, Message: "x"}, "me", "x"},
		{"from user", &tgapi.Message{FromID: &tgapi.PeerUser{UserID: 7}, Message: "x"}, "Ann", "x"},
		{"peer title", &tgapi.Message{Message: "x"}, "Chan", "x"},
		{"post author", &tgapi.Message{PostAuthor: "Ed", Message: "x"}, "Ed", "x"},
		{"service", &tgapi.MessageService{FromID: &tgapi.PeerUser{UserID: 7}, Action: &tgapi.MessageActionPinMessage{}}, "Ann", "[service: message pinned]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := RenderMessage(tt.m, names, "Chan")
			if !ok {
				t.Fatal("ok == false")
			}
			if got.From != tt.wantFrom || got.Text != tt.wantText {
				t.Errorf("got From %q Text %q, want %q %q", got.From, got.Text, tt.wantFrom, tt.wantText)
			}
		})
	}

	got, _ := RenderMessage(&tgapi.Message{ID: 42, Date: 1700000000, Message: "x"}, names, "Chan")
	if got.ID != 42 || got.Date != 1700000000 {
		t.Errorf("ID/Date not copied: %+v", got)
	}
}
