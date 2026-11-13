package tg

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/gotd/td/telegram/downloader"
	tgapi "github.com/gotd/td/tg"
)

var ErrMessageNotFound = errors.New("message not found")

// DownloadMedia saves the media of one message to <downloadDir>/<chat>_<msg>.<ext>.
// An existing file at that path is returned without downloading again.
func (s *Service) DownloadMedia(ctx context.Context, p DownloadMediaParams) (*DownloadMediaResult, error) {
	peer, err := s.resolvePeer(ctx, p.ChatType, p.ChatID)
	if err != nil {
		return nil, err
	}
	msg, err := s.getMessage(ctx, peer, p.MessageID)
	if err != nil {
		return nil, err
	}
	mf, err := mediaFileOf(msg.Media)
	if err != nil {
		return nil, err
	}
	if mf.Size > s.maxDownload {
		return nil, fmt.Errorf("file is %d bytes, limit is %d", mf.Size, s.maxDownload)
	}

	if err := os.MkdirAll(s.downloadDir, 0o700); err != nil {
		return nil, fmt.Errorf("create download dir: %w", err)
	}
	path := filepath.Join(s.downloadDir, fmt.Sprintf("%d_%d%s", p.ChatID, p.MessageID, mf.Ext))
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		if err := s.downloadTo(ctx, mf.Location, path); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	return &DownloadMediaResult{
		Path:     path,
		Kind:     mf.Kind,
		MimeType: mf.MimeType,
		Size:     info.Size(),
		FileName: mf.FileName,
		Duration: mf.Duration,
	}, nil
}

// getMessage fetches one message by id. Channels and supergroups need
// channels.getMessages; private chats and basic groups use messages.getMessages.
func (s *Service) getMessage(ctx context.Context, peer tgapi.InputPeerClass, id int) (*tgapi.Message, error) {
	ids := []tgapi.InputMessageClass{&tgapi.InputMessageID{ID: id}}
	var (
		resp tgapi.MessagesMessagesClass
		err  error
	)
	if ch, ok := peer.(*tgapi.InputPeerChannel); ok {
		resp, err = s.api.ChannelsGetMessages(ctx, &tgapi.ChannelsGetMessagesRequest{
			Channel: &tgapi.InputChannel{ChannelID: ch.ChannelID, AccessHash: ch.AccessHash},
			ID:      ids,
		})
	} else {
		resp, err = s.api.MessagesGetMessages(ctx, ids)
	}
	if err != nil {
		return nil, fmt.Errorf("get message: %w", err)
	}
	modified, ok := resp.AsModified()
	if !ok {
		return nil, ErrMessageNotFound
	}
	s.peers.PutFromResponse(modified.GetUsers(), modified.GetChats())
	if err := s.peers.Save(); err != nil {
		slog.Warn("save peer cache", "err", err)
	}
	for _, mc := range modified.GetMessages() {
		if m, ok := mc.(*tgapi.Message); ok && m.ID == id && inputPeerIs(peer, m.PeerID) {
			return m, nil
		}
	}
	return nil, ErrMessageNotFound
}

// inputPeerIs reports whether a message's peer is the requested chat.
// messages.getMessages ignores the chat: ids of private chats and basic
// groups are account-wide, so an id from another chat must be rejected.
func inputPeerIs(in tgapi.InputPeerClass, p tgapi.PeerClass) bool {
	switch in := in.(type) {
	case *tgapi.InputPeerUser:
		u, ok := p.(*tgapi.PeerUser)
		return ok && u.UserID == in.UserID
	case *tgapi.InputPeerChat:
		c, ok := p.(*tgapi.PeerChat)
		return ok && c.ChatID == in.ChatID
	case *tgapi.InputPeerChannel:
		c, ok := p.(*tgapi.PeerChannel)
		return ok && c.ChannelID == in.ChannelID
	}
	return false
}

// downloadTo writes the file to a temp file in the same directory and renames
// it on success, so a failed download never leaves a file at path.
func (s *Service) downloadTo(ctx context.Context, loc tgapi.InputFileLocationClass, path string) (err error) {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".dl-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()
	if _, err = downloader.NewDownloader().Download(s.api, loc).Stream(ctx, tmp); err != nil {
		return fmt.Errorf("download: %w", err)
	}
	if err = tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	if err = os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("rename temp file: %w", err)
	}
	return nil
}
