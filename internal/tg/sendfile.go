package tg

import (
	"context"
	"fmt"
	"io"
	"path/filepath"

	"github.com/gotd/td/telegram/message/unpack"
	"github.com/gotd/td/telegram/uploader"
	tgapi "github.com/gotd/td/tg"
)

const maxPhotoBytes = 10 << 20

// SendFile uploads a local file and sends it to a chat as the logged-in user.
// All path and format checks run before any Telegram call.
func (s *Service) SendFile(ctx context.Context, p SendFileParams) (*SendFileResult, error) {
	f, resolved, err := s.sendDirs.Open(p.Path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	st, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat file: %w", err)
	}
	size := st.Size()
	switch {
	case size == 0:
		return nil, fmt.Errorf("file is empty")
	case size > s.maxUpload:
		return nil, fmt.Errorf("file is %d bytes, limit is %d", size, s.maxUpload)
	case p.Kind == KindPhoto && size > maxPhotoBytes:
		return nil, fmt.Errorf("photo is %d bytes, Telegram limit is 10 MiB; send it as document", size)
	}

	name := filepath.Base(resolved)
	spec, err := buildMedia(p.Kind, f, size, name)
	if err != nil {
		return nil, err
	}

	var caption string
	var entities []tgapi.MessageEntityClass
	if p.Caption != "" {
		if caption, entities, err = parseMarkdown(p.Caption); err != nil {
			return nil, err
		}
	}

	peer, err := s.resolvePeer(ctx, p.ChatType, p.ChatID)
	if err != nil {
		return nil, err
	}

	file, err := uploader.NewUploader(s.api).Upload(ctx, uploader.NewUpload(name, io.NewSectionReader(f, 0, size), size))
	if err != nil {
		return nil, fmt.Errorf("upload: %w", err)
	}

	var media tgapi.InputMediaClass
	if spec.Photo {
		media = &tgapi.InputMediaUploadedPhoto{File: file}
	} else {
		media = &tgapi.InputMediaUploadedDocument{File: file, MimeType: spec.MimeType, Attributes: spec.Attributes, ForceFile: spec.ForceFile}
	}
	req := &tgapi.MessagesSendMediaRequest{Peer: peer, Media: media, Message: caption, RandomID: randomID()}
	if len(entities) > 0 {
		req.SetEntities(entities)
	}
	if p.ReplyTo != 0 {
		req.SetReplyTo(&tgapi.InputReplyToMessage{ReplyToMsgID: p.ReplyTo})
	}

	msg, err := unpack.Message(s.api.MessagesSendMedia(ctx, req))
	if err != nil {
		return nil, fmt.Errorf("messages.sendMedia: %w", err)
	}
	return &SendFileResult{ID: msg.ID, Date: int64(msg.Date)}, nil
}
