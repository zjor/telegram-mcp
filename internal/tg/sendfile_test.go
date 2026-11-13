package tg

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/gotd/td/bin"
	tgapi "github.com/gotd/td/tg"
	"github.com/zjor/telegram-mcp/internal/probe/probetest"
)

var pngMagic = []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}

func sendFileSetup(t *testing.T, maxBytes int64) (*Service, string) {
	t.Helper()
	s, _, _ := newTestService(t)
	dir := t.TempDir()
	s.SetUploads(NewSendDirs([]string{dir}, nil), maxBytes)
	return s, dir
}

func writeFile(t *testing.T, dir, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestSendFile_Voice(t *testing.T) {
	s, mock, _ := newTestService(t)
	dir := t.TempDir()
	s.SetUploads(NewSendDirs([]string{dir}, nil), 1<<20)
	s.peers.Put(ChatPrivate, 7, 9)
	path := writeFile(t, dir, "v.ogg", probetest.OggFile("opus", 48000, 312, 3*48000+312))

	mock.ExpectFunc(func(b bin.Encoder) {
		if _, ok := b.(*tgapi.UploadSaveFilePartRequest); !ok {
			t.Errorf("request type %T, want *UploadSaveFilePartRequest", b)
		}
	}).ThenTrue()
	mock.ExpectFunc(func(b bin.Encoder) {
		req, ok := b.(*tgapi.MessagesSendMediaRequest)
		if !ok {
			t.Fatalf("request type %T, want *MessagesSendMediaRequest", b)
		}
		peer, ok := req.Peer.(*tgapi.InputPeerUser)
		if !ok || peer.UserID != 7 || peer.AccessHash != 9 {
			t.Errorf("Peer = %#v", req.Peer)
		}
		doc, ok := req.Media.(*tgapi.InputMediaUploadedDocument)
		if !ok {
			t.Fatalf("Media = %#v, want InputMediaUploadedDocument", req.Media)
		}
		if doc.MimeType != "audio/ogg" || len(doc.Attributes) != 1 {
			t.Fatalf("doc = %#v", doc)
		}
		a, ok := doc.Attributes[0].(*tgapi.DocumentAttributeAudio)
		if !ok || !a.Voice || a.Duration != 3 {
			t.Errorf("attr = %#v", doc.Attributes[0])
		}
		if req.RandomID == 0 || req.Message != "" || req.ReplyTo != nil {
			t.Errorf("req = %#v", req)
		}
	}).ThenResult(&tgapi.UpdateShortSentMessage{ID: 42, Date: 500})

	res, err := s.SendFile(context.Background(), SendFileParams{ChatID: 7, ChatType: ChatPrivate, Path: path, Kind: KindVoice})
	if err != nil {
		t.Fatal(err)
	}
	if res.ID != 42 || res.Date != 500 {
		t.Fatalf("result = %+v", res)
	}
}

func TestSendFile_PhotoCaptionReply(t *testing.T) {
	s, mock, _ := newTestService(t)
	dir := t.TempDir()
	s.SetUploads(NewSendDirs([]string{dir}, nil), 1<<20)
	s.peers.Put(ChatPrivate, 7, 9)
	path := writeFile(t, dir, "a.png", append(append([]byte{}, pngMagic...), make([]byte, 16)...))

	mock.ExpectFunc(func(b bin.Encoder) {}).ThenTrue()
	mock.ExpectFunc(func(b bin.Encoder) {
		req := b.(*tgapi.MessagesSendMediaRequest)
		if _, ok := req.Media.(*tgapi.InputMediaUploadedPhoto); !ok {
			t.Errorf("Media = %#v, want InputMediaUploadedPhoto", req.Media)
		}
		if req.Message != "hi x" {
			t.Errorf("Message = %q", req.Message)
		}
		if len(req.Entities) != 1 {
			t.Fatalf("Entities = %#v", req.Entities)
		}
		if bold, ok := req.Entities[0].(*tgapi.MessageEntityBold); !ok || bold.Offset != 3 || bold.Length != 1 {
			t.Errorf("Entities[0] = %#v", req.Entities[0])
		}
		r, ok := req.ReplyTo.(*tgapi.InputReplyToMessage)
		if !ok || r.ReplyToMsgID != 5 {
			t.Errorf("ReplyTo = %#v", req.ReplyTo)
		}
	}).ThenResult(&tgapi.UpdateShortSentMessage{ID: 1, Date: 2})

	_, err := s.SendFile(context.Background(), SendFileParams{ChatID: 7, ChatType: ChatPrivate, Path: path, Kind: KindPhoto, Caption: "hi **x**", ReplyTo: 5})
	if err != nil {
		t.Fatal(err)
	}
}

func TestSendFile_OutsideDirs_NoNetwork(t *testing.T) {
	s, _, _ := newTestService(t)
	s.SetUploads(NewSendDirs([]string{t.TempDir()}, nil), 1<<20)
	s.peers.Put(ChatPrivate, 7, 9)
	path := writeFile(t, t.TempDir(), "a.txt", []byte("hi"))
	_, err := s.SendFile(context.Background(), SendFileParams{ChatID: 7, ChatType: ChatPrivate, Path: path, Kind: KindDocument})
	if !errors.Is(err, ErrOutsideSendDirs) {
		t.Fatalf("err = %v, want ErrOutsideSendDirs", err)
	}
}

func TestSendFile_Empty(t *testing.T) {
	s, dir := sendFileSetup(t, 1<<20)
	path := writeFile(t, dir, "e.txt", nil)
	_, err := s.SendFile(context.Background(), SendFileParams{ChatID: 7, ChatType: ChatPrivate, Path: path, Kind: KindDocument})
	if err == nil || err.Error() != "file is empty" {
		t.Fatalf("err = %v", err)
	}
}

func TestSendFile_TooBig(t *testing.T) {
	s, dir := sendFileSetup(t, 10)
	path := writeFile(t, dir, "b.txt", make([]byte, 11))
	_, err := s.SendFile(context.Background(), SendFileParams{ChatID: 7, ChatType: ChatPrivate, Path: path, Kind: KindDocument})
	if err == nil || err.Error() != "file is 11 bytes, limit is 10" {
		t.Fatalf("err = %v", err)
	}
}

func TestSendFile_PhotoOver10MiB(t *testing.T) {
	s, dir := sendFileSetup(t, 20<<20)
	p := filepath.Join(dir, "big.png")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(pngMagic); err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(10<<20 + 1); err != nil {
		t.Fatal(err)
	}
	f.Close()
	_, err = s.SendFile(context.Background(), SendFileParams{ChatID: 7, ChatType: ChatPrivate, Path: p, Kind: KindPhoto})
	want := "photo is 10485761 bytes, Telegram limit is 10 MiB; send it as document"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

func TestSendFile_BadFormat_NoNetwork(t *testing.T) {
	s, dir := sendFileSetup(t, 1<<20)
	path := writeFile(t, dir, "a.txt", []byte("hello"))
	_, err := s.SendFile(context.Background(), SendFileParams{ChatID: 7, ChatType: ChatPrivate, Path: path, Kind: KindVoice})
	if err == nil || err.Error() != errVoice {
		t.Fatalf("err = %v", err)
	}
}

func TestSendFile_UnknownChat(t *testing.T) {
	s, mock, _ := newTestService(t)
	dir := t.TempDir()
	s.SetUploads(NewSendDirs([]string{dir}, nil), 1<<20)
	path := writeFile(t, dir, "a.txt", []byte("hello"))
	mock.ExpectFunc(func(b bin.Encoder) { dialogsRequest(t, b) }).ThenResult(&tgapi.MessagesDialogs{})
	mock.ExpectFunc(func(b bin.Encoder) { dialogsRequest(t, b) }).ThenResult(&tgapi.MessagesDialogs{})
	_, err := s.SendFile(context.Background(), SendFileParams{ChatID: 99, ChatType: ChatPrivate, Path: path, Kind: KindDocument})
	if !errors.Is(err, ErrUnknownChat) {
		t.Fatalf("err = %v, want ErrUnknownChat", err)
	}
}
