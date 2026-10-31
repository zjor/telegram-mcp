package tg

import (
	"strings"

	tgapi "github.com/gotd/td/tg"
)

const deletedAccount = "Deleted Account"

// Names resolves sender/peer display names from the users/chats arrays of one RPC response.
type Names struct {
	users    map[int64]*tgapi.User
	chats    map[int64]tgapi.ChatClass // Chat, ChatForbidden
	channels map[int64]tgapi.ChatClass // Channel, ChannelForbidden
}

func NewNames(users []tgapi.UserClass, chats []tgapi.ChatClass) *Names {
	n := &Names{
		users:    map[int64]*tgapi.User{},
		chats:    map[int64]tgapi.ChatClass{},
		channels: map[int64]tgapi.ChatClass{},
	}
	for _, uc := range users {
		if u, ok := uc.(*tgapi.User); ok {
			n.users[u.ID] = u
		}
	}
	for _, c := range chats {
		switch c.(type) {
		case *tgapi.Channel, *tgapi.ChannelForbidden:
			n.channels[c.GetID()] = c
		default:
			n.chats[c.GetID()] = c
		}
	}
	return n
}

// PeerName returns "" for a nil peer or an unknown chat/channel, and
// "Deleted Account" for a deleted, empty or unknown user.
func (n *Names) PeerName(p tgapi.PeerClass) string {
	switch p := p.(type) {
	case *tgapi.PeerUser:
		u, ok := n.users[p.UserID]
		if !ok {
			return deletedAccount
		}
		return UserName(u)
	case *tgapi.PeerChat:
		return chatTitle(n.chats[p.ChatID])
	case *tgapi.PeerChannel:
		return chatTitle(n.channels[p.ChannelID])
	}
	return ""
}

func chatTitle(c tgapi.ChatClass) string {
	switch c := c.(type) {
	case *tgapi.Chat:
		return c.Title
	case *tgapi.ChatForbidden:
		return c.Title
	case *tgapi.Channel:
		return c.Title
	case *tgapi.ChannelForbidden:
		return c.Title
	}
	return ""
}

func UserName(u *tgapi.User) string {
	if u.Deleted {
		return deletedAccount
	}
	return strings.TrimSpace(u.FirstName + " " + u.LastName)
}

func RenderText(m *tgapi.Message) string {
	placeholder := mediaPlaceholder(m.Media)
	switch {
	case placeholder == "":
		return m.Message
	case m.Message == "":
		return placeholder
	}
	return placeholder + " " + m.Message
}

func mediaPlaceholder(media tgapi.MessageMediaClass) string {
	switch md := media.(type) {
	case *tgapi.MessageMediaPhoto:
		return "[photo]"
	case *tgapi.MessageMediaDocument:
		return documentPlaceholder(md)
	case *tgapi.MessageMediaGeo, *tgapi.MessageMediaGeoLive:
		return "[location]"
	case *tgapi.MessageMediaContact:
		return "[contact]"
	case *tgapi.MessageMediaPoll:
		return "[poll]"
	case *tgapi.MessageMediaUnsupported:
		return "[media]"
	}
	// nil media and web page previews are not media.
	return ""
}

func documentPlaceholder(md *tgapi.MessageMediaDocument) string {
	doc, ok := md.Document.(*tgapi.Document)
	if !ok {
		return "[document]"
	}
	var (
		sticker, voice, audio, videoNote, video, animated bool
		filename                                          string
	)
	for _, a := range doc.Attributes {
		switch a := a.(type) {
		case *tgapi.DocumentAttributeSticker:
			sticker = true
		case *tgapi.DocumentAttributeAudio:
			if a.Voice {
				voice = true
			} else {
				audio = true
			}
		case *tgapi.DocumentAttributeVideo:
			if a.RoundMessage {
				videoNote = true
			} else {
				video = true
			}
		case *tgapi.DocumentAttributeAnimated:
			animated = true
		case *tgapi.DocumentAttributeFilename:
			filename = a.FileName
		}
	}
	switch {
	case sticker:
		return "[sticker]"
	case voice:
		return "[voice]"
	case audio:
		return "[audio]"
	case videoNote:
		return "[video_note]"
	case video:
		return "[video]"
	case animated:
		return "[gif]"
	case filename != "":
		return "[document " + filename + "]"
	}
	return "[document]"
}

func RenderService(m *tgapi.MessageService) string {
	switch a := m.Action.(type) {
	case *tgapi.MessageActionPinMessage:
		return "[service: message pinned]"
	case *tgapi.MessageActionChatEditTitle:
		return "[service: title changed to " + a.Title + "]"
	case *tgapi.MessageActionChatAddUser:
		return "[service: user added]"
	case *tgapi.MessageActionChatDeleteUser:
		return "[service: user removed]"
	case *tgapi.MessageActionPhoneCall:
		return "[service: call]"
	case *tgapi.MessageActionChatJoinedByLink:
		return "[service: user joined]"
	}
	return "[service]"
}

// RenderMessage returns ok == false for *tgapi.MessageEmpty and unknown types.
func RenderMessage(mc tgapi.MessageClass, names *Names, peerTitle string) (Message, bool) {
	switch m := mc.(type) {
	case *tgapi.Message:
		return Message{
			ID:   m.ID,
			Date: int64(m.Date),
			From: sender(m.Out, m.FromID, m.PostAuthor, names, peerTitle),
			Text: RenderText(m),
		}, true
	case *tgapi.MessageService:
		return Message{
			ID:   m.ID,
			Date: int64(m.Date),
			From: sender(m.Out, m.FromID, "", names, peerTitle),
			Text: RenderService(m),
		}, true
	}
	return Message{}, false
}

func sender(out bool, from tgapi.PeerClass, postAuthor string, names *Names, peerTitle string) string {
	switch {
	case out:
		return "me"
	case from != nil:
		return names.PeerName(from)
	case postAuthor != "":
		return postAuthor
	}
	return peerTitle
}
