package tg

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gotd/td/bin"
	"github.com/gotd/td/tgerr"
	"github.com/gotd/td/tgmock"
	tgapi "github.com/gotd/td/tg"
)

func voiceMessage(id int, size int64) *tgapi.Message {
	return &tgapi.Message{ID: id, PeerID: &tgapi.PeerUser{UserID: 7}, Media: docMedia("audio/ogg", size, &tgapi.DocumentAttributeAudio{Voice: true, Duration: 7})}
}

func newDownloadService(t *testing.T) (*Service, *tgmock.Mock, string) {
	t.Helper()
	s, mock, _ := newTestService(t)
	dir := filepath.Join(t.TempDir(), "dl")
	s.SetDownloads(dir, 1<<20)
	return s, mock, dir
}

func expectGetMessages(t *testing.T, mock *tgmock.Mock, wantID int, msgs ...tgapi.MessageClass) {
	t.Helper()
	mock.ExpectFunc(func(b bin.Encoder) {
		req, ok := b.(*tgapi.MessagesGetMessagesRequest)
		if !ok {
			t.Fatalf("request type %T, want *MessagesGetMessagesRequest", b)
		}
		if len(req.ID) != 1 {
			t.Fatalf("ID = %#v, want one id", req.ID)
		}
		if id, ok := req.ID[0].(*tgapi.InputMessageID); !ok || id.ID != wantID {
			t.Errorf("ID[0] = %#v, want InputMessageID{%d}", req.ID[0], wantID)
		}
	}).ThenResult(&tgapi.MessagesMessages{Messages: msgs})
}

func expectGetFile(t *testing.T, mock *tgmock.Mock, data string) {
	t.Helper()
	mock.ExpectFunc(func(b bin.Encoder) {
		req, ok := b.(*tgapi.UploadGetFileRequest)
		if !ok {
			t.Fatalf("request type %T, want *UploadGetFileRequest", b)
		}
		loc, ok := req.Location.(*tgapi.InputDocumentFileLocation)
		if !ok || loc.ID != 1 || loc.AccessHash != 2 {
			t.Errorf("Location = %#v, want InputDocumentFileLocation{1, 2}", req.Location)
		}
	}).ThenResult(&tgapi.UploadFile{Type: &tgapi.StorageFilePartial{}, Bytes: []byte(data)})
}

func TestDownloadMedia_PrivateVoice(t *testing.T) {
	s, mock, dir := newDownloadService(t)
	s.peers.Put(ChatPrivate, 7, 9)
	expectGetMessages(t, mock, 10, voiceMessage(10, 3))
	expectGetFile(t, mock, "abc")

	res, err := s.DownloadMedia(context.Background(), DownloadMediaParams{ChatID: 7, ChatType: ChatPrivate, MessageID: 10})
	if err != nil {
		t.Fatal(err)
	}
	want := DownloadMediaResult{Path: filepath.Join(dir, "7_10.ogg"), Kind: "voice", MimeType: "audio/ogg", Size: 3, Duration: 7}
	if *res != want {
		t.Fatalf("result = %+v, want %+v", *res, want)
	}
	data, err := os.ReadFile(res.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "abc" {
		t.Fatalf("file = %q, want abc", data)
	}
}

func TestDownloadMedia_ChannelUsesChannelsGetMessages(t *testing.T) {
	s, mock, _ := newDownloadService(t)
	s.peers.Put(ChatChannel, 5, 11)
	mock.ExpectFunc(func(b bin.Encoder) {
		req, ok := b.(*tgapi.ChannelsGetMessagesRequest)
		if !ok {
			t.Fatalf("request type %T, want *ChannelsGetMessagesRequest", b)
		}
		ch, ok := req.Channel.(*tgapi.InputChannel)
		if !ok || ch.ChannelID != 5 || ch.AccessHash != 11 {
			t.Errorf("Channel = %#v, want InputChannel{5, 11}", req.Channel)
		}
	}).ThenResult(&tgapi.MessagesChannelMessages{Messages: []tgapi.MessageClass{voiceMessage(10, 3)}})
	expectGetFile(t, mock, "abc")

	res, err := s.DownloadMedia(context.Background(), DownloadMediaParams{ChatID: 5, ChatType: ChatChannel, MessageID: 10})
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(res.Path) != "5_10.ogg" {
		t.Fatalf("Path = %q, want .../5_10.ogg", res.Path)
	}
}

func TestDownloadMedia_ExistingFileSkipsDownload(t *testing.T) {
	s, mock, dir := newDownloadService(t)
	s.peers.Put(ChatPrivate, 7, 9)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "7_10.ogg"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	expectGetMessages(t, mock, 10, voiceMessage(10, 3))

	res, err := s.DownloadMedia(context.Background(), DownloadMediaParams{ChatID: 7, ChatType: ChatPrivate, MessageID: 10})
	if err != nil {
		t.Fatal(err)
	}
	if res.Path != filepath.Join(dir, "7_10.ogg") || res.Size != 3 {
		t.Fatalf("result = %+v", *res)
	}
}

func TestDownloadMedia_TooLarge(t *testing.T) {
	s, mock, dir := newDownloadService(t)
	s.peers.Put(ChatPrivate, 7, 9)
	expectGetMessages(t, mock, 10, voiceMessage(10, 2<<20))

	_, err := s.DownloadMedia(context.Background(), DownloadMediaParams{ChatID: 7, ChatType: ChatPrivate, MessageID: 10})
	if err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("err = %v, want size limit error", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("dir has %d entries, want none", len(entries))
	}
}

func TestDownloadMedia_MessageNotFound(t *testing.T) {
	s, mock, _ := newDownloadService(t)
	s.peers.Put(ChatPrivate, 7, 9)
	expectGetMessages(t, mock, 10, &tgapi.MessageEmpty{ID: 10})

	_, err := s.DownloadMedia(context.Background(), DownloadMediaParams{ChatID: 7, ChatType: ChatPrivate, MessageID: 10})
	if !errors.Is(err, ErrMessageNotFound) {
		t.Fatalf("err = %v, want ErrMessageNotFound", err)
	}
}

func TestDownloadMedia_NoMedia(t *testing.T) {
	s, mock, _ := newDownloadService(t)
	s.peers.Put(ChatPrivate, 7, 9)
	expectGetMessages(t, mock, 10, &tgapi.Message{ID: 10, PeerID: &tgapi.PeerUser{UserID: 7}, Message: "hi"})

	_, err := s.DownloadMedia(context.Background(), DownloadMediaParams{ChatID: 7, ChatType: ChatPrivate, MessageID: 10})
	if !errors.Is(err, ErrNoMedia) {
		t.Fatalf("err = %v, want ErrNoMedia", err)
	}
}

func TestDownloadMedia_FailedDownloadLeavesNoFile(t *testing.T) {
	s, mock, dir := newDownloadService(t)
	s.peers.Put(ChatPrivate, 7, 9)
	expectGetMessages(t, mock, 10, voiceMessage(10, 3))
	mock.Expect().ThenRPCErr(&tgerr.Error{Code: 400, Message: "FILE_REFERENCE_EXPIRED", Type: "FILE_REFERENCE_EXPIRED"})

	_, err := s.DownloadMedia(context.Background(), DownloadMediaParams{ChatID: 7, ChatType: ChatPrivate, MessageID: 10})
	if err == nil {
		t.Fatal("err = nil, want download error")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("dir has %d entries, want none", len(entries))
	}
}
