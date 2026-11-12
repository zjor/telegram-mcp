package tg

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	tgapi "github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
)

var (
	ErrUnknownUser      = errors.New("unknown user; pass a chat_id of a private chat from list_chats, or a username")
	ErrUsernameNotFound = errors.New("username not found")
	ErrNotAUser         = errors.New("username belongs to a group or channel, not a user")
)

// GetUser returns the profile details Telegram shows for one user. Exactly
// one of p.UserID and p.Username is set; Username carries no @.
func (s *Service) GetUser(ctx context.Context, p GetUserParams) (*UserInfo, error) {
	var (
		in  *tgapi.InputUser
		err error
	)
	if p.Username != "" {
		in, err = s.resolveUsername(ctx, p.Username)
	} else {
		in, err = s.inputUserByID(ctx, p.UserID)
	}
	if err != nil {
		return nil, err
	}

	full, err := s.api.UsersGetFullUser(ctx, in)
	if err != nil {
		return nil, fmt.Errorf("users.getFullUser: %w", err)
	}
	s.cacheAndSave(full.Users, full.Chats)

	for _, uc := range full.Users {
		if u, ok := uc.(*tgapi.User); ok && u.ID == in.UserID {
			info := newUserInfo(u, &full.FullUser)
			return &info, nil
		}
	}
	return nil, fmt.Errorf("users.getFullUser: user %d not in response", in.UserID)
}

func (s *Service) inputUserByID(ctx context.Context, id int64) (*tgapi.InputUser, error) {
	peer, err := s.resolvePeer(ctx, ChatPrivate, id)
	if errors.Is(err, ErrUnknownChat) {
		return nil, ErrUnknownUser
	}
	if err != nil {
		return nil, err
	}
	pu, ok := peer.(*tgapi.InputPeerUser)
	if !ok {
		return nil, ErrUnknownUser
	}
	return &tgapi.InputUser{UserID: pu.UserID, AccessHash: pu.AccessHash}, nil
}

func (s *Service) resolveUsername(ctx context.Context, username string) (*tgapi.InputUser, error) {
	res, err := s.api.ContactsResolveUsername(ctx, &tgapi.ContactsResolveUsernameRequest{Username: username})
	if tgerr.Is(err, "USERNAME_NOT_OCCUPIED", "USERNAME_INVALID") {
		return nil, ErrUsernameNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("contacts.resolveUsername: %w", err)
	}
	s.cacheAndSave(res.Users, res.Chats)

	pu, ok := res.Peer.(*tgapi.PeerUser)
	if !ok {
		return nil, ErrNotAUser
	}
	for _, uc := range res.Users {
		if u, ok := uc.(*tgapi.User); ok && u.ID == pu.UserID {
			return &tgapi.InputUser{UserID: u.ID, AccessHash: u.AccessHash}, nil
		}
	}
	return nil, fmt.Errorf("contacts.resolveUsername: user %d not in response", pu.UserID)
}

func (s *Service) cacheAndSave(users []tgapi.UserClass, chats []tgapi.ChatClass) {
	s.peers.PutFromResponse(users, chats)
	if err := s.peers.Save(); err != nil {
		slog.Warn("save peer cache", "err", err)
	}
}
