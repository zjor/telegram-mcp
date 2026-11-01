package tg

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"strings"

	"github.com/gotd/td/telegram/message/entity"
	"github.com/gotd/td/telegram/message/markdown"
	"github.com/gotd/td/telegram/message/unpack"
	tgapi "github.com/gotd/td/tg"
)

// SendMessage sends Markdown text to a chat as the logged-in user.
func (s *Service) SendMessage(ctx context.Context, p SendMessageParams) (*SendMessageResult, error) {
	peer, err := s.resolvePeer(ctx, p.ChatType, p.ChatID)
	if err != nil {
		return nil, err
	}

	var b entity.Builder
	if err := markdown.Markdown(strings.NewReader(p.Text), &b, markdown.Options{}); err != nil {
		return nil, fmt.Errorf("parse markdown: %w", err)
	}
	text, entities := b.Complete()

	req := &tgapi.MessagesSendMessageRequest{
		Peer:     peer,
		Message:  text,
		RandomID: randomID(),
	}
	if len(entities) > 0 {
		req.SetEntities(entities)
	}
	if p.ReplyTo != 0 {
		req.SetReplyTo(&tgapi.InputReplyToMessage{ReplyToMsgID: p.ReplyTo})
	}

	msg, err := unpack.Message(s.api.MessagesSendMessage(ctx, req))
	if err != nil {
		return nil, fmt.Errorf("messages.sendMessage: %w", err)
	}
	return &SendMessageResult{ID: msg.ID, Date: int64(msg.Date)}, nil
}

func randomID() int64 {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		panic(err) // crypto/rand never fails on supported platforms
	}
	return int64(binary.LittleEndian.Uint64(buf[:]))
}
