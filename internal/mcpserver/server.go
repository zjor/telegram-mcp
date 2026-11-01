// Package mcpserver exposes the Telegram backend as MCP tools.
package mcpserver

import (
	"context"
	"errors"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zjor/telegram-mcp/internal/tg"
)

// Version is the server version reported in the MCP handshake.
const Version = "0.1.0"

// Telegram is the backend the tools call.
type Telegram interface {
	ListChats(ctx context.Context, p tg.ListChatsParams) (*tg.ListChatsResult, error)
	ReadChat(ctx context.Context, p tg.ReadChatParams) (*tg.ReadChatResult, error)
	SendMessage(ctx context.Context, p tg.SendMessageParams) (*tg.SendMessageResult, error)
}

type ListChatsInput struct {
	Folder string `json:"folder,omitempty" jsonschema:"main or archive; default main"`
	Limit  int    `json:"limit,omitempty"  jsonschema:"page size 1-100; default 50"`
	Cursor string `json:"cursor,omitempty" jsonschema:"opaque cursor from a previous call"`
}

type ReadChatInput struct {
	ChatID    int64  `json:"chat_id"             jsonschema:"id from list_chats"`
	ChatType  string `json:"chat_type"           jsonschema:"private, group, supergroup or channel"`
	Limit     int    `json:"limit,omitempty"     jsonschema:"page size 1-100; default 50"`
	BeforeID  int    `json:"before_id,omitempty" jsonschema:"only messages with id lower than this"`
	SinceDate int64  `json:"since_date,omitempty" jsonschema:"unix seconds; return nothing older"`
}

type SendMessageInput struct {
	ChatID   int64  `json:"chat_id"            jsonschema:"id from list_chats"`
	ChatType string `json:"chat_type"          jsonschema:"private, group, supergroup or channel"`
	Text     string `json:"text"               jsonschema:"message text in Markdown"`
	ReplyTo  int    `json:"reply_to,omitempty" jsonschema:"id of the message to reply to"`
}

const (
	sendMessageDescription = "Send a text message to one chat as the logged-in user. Text is Markdown: **bold**, *italic*, ~~strikethrough~~, ||spoiler||, `code`, ```fenced code```, [link](url), > quote. Unmatched markers are sent as plain text. Pass reply_to to reply to a message id from read_chat."
	listChatsDescription   = "List the user's Telegram chats (private chats, groups, supergroups, channels), newest activity first. Use the returned cursor to fetch the next page. Pass folder=archive to list archived chats."
	readChatDescription    = "Read plain-text message history of one chat, newest first. Pass before_id=next_before_id from the previous call to page backwards; stop when next_before_id is null."
)

// New builds an MCP server with the list_chats, read_chat and send_message tools.
func New(t Telegram) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "telegram-mcp", Version: Version}, nil)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_chats",
		Description: listChatsDescription,
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, listChatsHandler(t))

	mcp.AddTool(s, &mcp.Tool{
		Name:        "read_chat",
		Description: readChatDescription,
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, readChatHandler(t))

	mcp.AddTool(s, &mcp.Tool{
		Name:        "send_message",
		Description: sendMessageDescription,
	}, sendMessageHandler(t))

	return s
}

func sendMessageHandler(t Telegram) mcp.ToolHandlerFor[SendMessageInput, *tg.SendMessageResult] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in SendMessageInput) (*mcp.CallToolResult, *tg.SendMessageResult, error) {
		if in.ChatID == 0 {
			return nil, nil, errors.New("chat_id is required")
		}
		chatType, err := parseChatType(in.ChatType)
		if err != nil {
			return nil, nil, err
		}
		if strings.TrimSpace(in.Text) == "" {
			return nil, nil, errors.New("text is required")
		}
		res, err := t.SendMessage(ctx, tg.SendMessageParams{
			ChatID:   in.ChatID,
			ChatType: chatType,
			Text:     in.Text,
			ReplyTo:  in.ReplyTo,
		})
		if err != nil {
			return nil, nil, err
		}
		return nil, res, nil
	}
}

func listChatsHandler(t Telegram) mcp.ToolHandlerFor[ListChatsInput, *tg.ListChatsResult] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in ListChatsInput) (*mcp.CallToolResult, *tg.ListChatsResult, error) {
		folder, err := parseFolder(in.Folder)
		if err != nil {
			return nil, nil, err
		}
		limit, err := parseLimit(in.Limit)
		if err != nil {
			return nil, nil, err
		}
		res, err := t.ListChats(ctx, tg.ListChatsParams{Folder: folder, Limit: limit, Cursor: in.Cursor})
		if err != nil {
			return nil, nil, err
		}
		return nil, res, nil
	}
}

func readChatHandler(t Telegram) mcp.ToolHandlerFor[ReadChatInput, *tg.ReadChatResult] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in ReadChatInput) (*mcp.CallToolResult, *tg.ReadChatResult, error) {
		if in.ChatID == 0 {
			return nil, nil, errors.New("chat_id is required")
		}
		chatType, err := parseChatType(in.ChatType)
		if err != nil {
			return nil, nil, err
		}
		limit, err := parseLimit(in.Limit)
		if err != nil {
			return nil, nil, err
		}
		res, err := t.ReadChat(ctx, tg.ReadChatParams{
			ChatID:    in.ChatID,
			ChatType:  chatType,
			Limit:     limit,
			BeforeID:  in.BeforeID,
			SinceDate: in.SinceDate,
		})
		if err != nil {
			return nil, nil, err
		}
		return nil, res, nil
	}
}

func parseFolder(s string) (tg.Folder, error) {
	switch tg.Folder(s) {
	case "", tg.FolderMain:
		return tg.FolderMain, nil
	case tg.FolderArchive:
		return tg.FolderArchive, nil
	}
	return "", errors.New("folder must be main or archive")
}

func parseLimit(n int) (int, error) {
	if n == 0 {
		return tg.DefaultLimit, nil
	}
	if n < 0 || n > tg.MaxLimit {
		return 0, errors.New("limit must be between 1 and 100")
	}
	return n, nil
}

func parseChatType(s string) (tg.ChatType, error) {
	switch ct := tg.ChatType(s); ct {
	case tg.ChatPrivate, tg.ChatGroup, tg.ChatSupergroup, tg.ChatChannel:
		return ct, nil
	}
	return "", errors.New("chat_type must be one of private, group, supergroup, channel")
}
