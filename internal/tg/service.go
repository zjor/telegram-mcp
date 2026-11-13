package tg

import (
	"errors"

	tgapi "github.com/gotd/td/tg"
)

var ErrUnknownChat = errors.New("unknown chat; call list_chats first")

// Service runs Telegram API operations for the MCP tools.
type Service struct {
	api   *tgapi.Client
	peers *PeerCache

	downloadDir string
	maxDownload int64 // bytes

	sendDirs  SendDirs
	maxUpload int64 // bytes
}

// SetUploads sets the folders send_file may read from and the largest file it uploads.
func (s *Service) SetUploads(dirs SendDirs, maxBytes int64) {
	s.sendDirs, s.maxUpload = dirs, maxBytes
}

// SetDownloads sets where download_media saves files and the largest file it downloads.
func (s *Service) SetDownloads(dir string, maxBytes int64) {
	s.downloadDir, s.maxDownload = dir, maxBytes
}

func NewService(inv tgapi.Invoker, peers *PeerCache) *Service {
	return &Service{api: tgapi.NewClient(inv), peers: peers}
}
