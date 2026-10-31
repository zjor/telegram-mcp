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
}

func NewService(inv tgapi.Invoker, peers *PeerCache) *Service {
	return &Service{api: tgapi.NewClient(inv), peers: peers}
}
