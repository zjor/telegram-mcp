package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zjor/telegram-mcp/internal/tg"
)

type fakeTelegram struct {
	listCalled bool
	listParams tg.ListChatsParams
	listResult *tg.ListChatsResult
	listErr    error

	readCalled bool
	readParams tg.ReadChatParams
	readResult *tg.ReadChatResult
	readErr    error

	sendCalled bool
	sendParams tg.SendMessageParams
	sendResult *tg.SendMessageResult
	sendErr    error
}

func (f *fakeTelegram) SendMessage(_ context.Context, p tg.SendMessageParams) (*tg.SendMessageResult, error) {
	f.sendCalled = true
	f.sendParams = p
	if f.sendErr != nil {
		return nil, f.sendErr
	}
	return f.sendResult, nil
}

func (f *fakeTelegram) ListChats(_ context.Context, p tg.ListChatsParams) (*tg.ListChatsResult, error) {
	f.listCalled = true
	f.listParams = p
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.listResult, nil
}

func (f *fakeTelegram) ReadChat(_ context.Context, p tg.ReadChatParams) (*tg.ReadChatResult, error) {
	f.readCalled = true
	f.readParams = p
	if f.readErr != nil {
		return nil, f.readErr
	}
	return f.readResult, nil
}

func newSession(t *testing.T, fake *fakeTelegram) *mcp.ClientSession {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	srv := New(fake)
	ct, st := mcp.NewInMemoryTransports()
	go func() { _ = srv.Run(ctx, st) }()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		cancel()
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() {
		_ = cs.Close()
		cancel()
	})
	return cs
}

func callTool(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool %s: %v", name, err)
	}
	return res
}

func resultText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	if len(res.Content) == 0 {
		t.Fatalf("result has no content")
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("content[0] is %T, want *mcp.TextContent", res.Content[0])
	}
	return tc.Text
}

func expectToolError(t *testing.T, res *mcp.CallToolResult, want string) {
	t.Helper()
	if !res.IsError {
		t.Fatalf("IsError = false, want true")
	}
	if got := resultText(t, res); !strings.Contains(got, want) {
		t.Fatalf("error text %q does not contain %q", got, want)
	}
}

func strPtr(s string) *string { return &s }

func TestListTools(t *testing.T) {
	cs := newSession(t, &fakeTelegram{})
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	got := map[string]bool{}
	for _, tool := range res.Tools {
		readOnly := tool.Annotations != nil && tool.Annotations.ReadOnlyHint
		got[tool.Name] = readOnly
	}
	want := map[string]bool{"list_chats": true, "read_chat": true, "send_message": false}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tools = %v, want %v", got, want)
	}
}

func TestListChats_DefaultsAndPassthrough(t *testing.T) {
	canned := &tg.ListChatsResult{
		Chats:      []tg.Chat{{ID: 1, Type: tg.ChatPrivate, Title: "Alice", LastMessageDate: 100, UnreadCount: 2, Folder: tg.FolderMain}},
		NextCursor: strPtr("next"),
	}
	fake := &fakeTelegram{listResult: canned}
	cs := newSession(t, fake)

	res := callTool(t, cs, "list_chats", map[string]any{})
	if res.IsError {
		t.Fatalf("IsError = true: %s", resultText(t, res))
	}
	want := tg.ListChatsParams{Folder: tg.FolderMain, Limit: tg.DefaultLimit, Cursor: ""}
	if fake.listParams != want {
		t.Fatalf("params = %+v, want %+v", fake.listParams, want)
	}
	var got tg.ListChatsResult
	if err := json.Unmarshal([]byte(resultText(t, res)), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !reflect.DeepEqual(&got, canned) {
		t.Fatalf("result = %+v, want %+v", got, *canned)
	}
}

func TestListChats_ArchiveAndCursor(t *testing.T) {
	fake := &fakeTelegram{listResult: &tg.ListChatsResult{Chats: []tg.Chat{}}}
	cs := newSession(t, fake)

	res := callTool(t, cs, "list_chats", map[string]any{"folder": "archive", "limit": 10, "cursor": "abc"})
	if res.IsError {
		t.Fatalf("IsError = true: %s", resultText(t, res))
	}
	want := tg.ListChatsParams{Folder: tg.FolderArchive, Limit: 10, Cursor: "abc"}
	if fake.listParams != want {
		t.Fatalf("params = %+v, want %+v", fake.listParams, want)
	}
}

func TestListChats_InvalidFolder(t *testing.T) {
	fake := &fakeTelegram{}
	cs := newSession(t, fake)

	res := callTool(t, cs, "list_chats", map[string]any{"folder": "spam"})
	expectToolError(t, res, "folder must be main or archive")
	if fake.listCalled {
		t.Fatalf("backend called on invalid input")
	}
}

func TestListChats_LimitTooLarge(t *testing.T) {
	fake := &fakeTelegram{}
	cs := newSession(t, fake)

	res := callTool(t, cs, "list_chats", map[string]any{"limit": 101})
	expectToolError(t, res, "limit must be between 1 and 100")
	if fake.listCalled {
		t.Fatalf("backend called on invalid input")
	}
}

func TestReadChat_Passthrough(t *testing.T) {
	fake := &fakeTelegram{readResult: &tg.ReadChatResult{Messages: []tg.Message{}}}
	cs := newSession(t, fake)

	res := callTool(t, cs, "read_chat", map[string]any{"chat_id": 7, "chat_type": "private", "before_id": 20, "since_date": 5})
	if res.IsError {
		t.Fatalf("IsError = true: %s", resultText(t, res))
	}
	want := tg.ReadChatParams{ChatID: 7, ChatType: tg.ChatPrivate, Limit: tg.DefaultLimit, BeforeID: 20, SinceDate: 5}
	if fake.readParams != want {
		t.Fatalf("params = %+v, want %+v", fake.readParams, want)
	}
}

func TestReadChat_InvalidType(t *testing.T) {
	fake := &fakeTelegram{}
	cs := newSession(t, fake)

	res := callTool(t, cs, "read_chat", map[string]any{"chat_id": 7, "chat_type": "dm"})
	expectToolError(t, res, "chat_type must be one of private, group, supergroup, channel")
	if fake.readCalled {
		t.Fatalf("backend called on invalid input")
	}
}

func TestReadChat_MissingChatID(t *testing.T) {
	fake := &fakeTelegram{}
	cs := newSession(t, fake)

	// Absent chat_id is rejected by the SDK's schema check (the field is
	// required in the inferred schema) before the handler runs.
	res := callTool(t, cs, "read_chat", map[string]any{"chat_type": "private"})
	expectToolError(t, res, "chat_id")
	// An explicit zero passes the schema and is rejected by the handler.
	res = callTool(t, cs, "read_chat", map[string]any{"chat_id": 0, "chat_type": "private"})
	expectToolError(t, res, "chat_id is required")
	if fake.readCalled {
		t.Fatalf("backend called on invalid input")
	}
}

func TestReadChat_BackendError(t *testing.T) {
	fake := &fakeTelegram{readErr: errors.New("boom")}
	cs := newSession(t, fake)

	res := callTool(t, cs, "read_chat", map[string]any{"chat_id": 7, "chat_type": "group"})
	expectToolError(t, res, "boom")
}

func TestReadChat_NullNextBeforeID(t *testing.T) {
	fake := &fakeTelegram{readResult: &tg.ReadChatResult{
		Messages:     []tg.Message{{ID: 3, Date: 10, From: "Bob", Text: "hi"}},
		NextBeforeID: nil,
	}}
	cs := newSession(t, fake)

	res := callTool(t, cs, "read_chat", map[string]any{"chat_id": 7, "chat_type": "channel"})
	if res.IsError {
		t.Fatalf("IsError = true: %s", resultText(t, res))
	}
	if txt := resultText(t, res); !strings.Contains(txt, `"next_before_id":null`) {
		t.Fatalf("text %q does not contain next_before_id null", txt)
	}
}

func TestSendMessage_Passthrough(t *testing.T) {
	fake := &fakeTelegram{sendResult: &tg.SendMessageResult{ID: 42, Date: 500}}
	cs := newSession(t, fake)

	res := callTool(t, cs, "send_message", map[string]any{"chat_id": 7, "chat_type": "private", "text": "hi **there**", "reply_to": 10})
	if res.IsError {
		t.Fatalf("IsError = true: %s", resultText(t, res))
	}
	want := tg.SendMessageParams{ChatID: 7, ChatType: tg.ChatPrivate, Text: "hi **there**", ReplyTo: 10}
	if fake.sendParams != want {
		t.Fatalf("params = %+v, want %+v", fake.sendParams, want)
	}
	var got tg.SendMessageResult
	if err := json.Unmarshal([]byte(resultText(t, res)), &got); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if got != *fake.sendResult {
		t.Fatalf("result = %+v, want %+v", got, *fake.sendResult)
	}
}

func TestSendMessage_Validation(t *testing.T) {
	fake := &fakeTelegram{}
	cs := newSession(t, fake)

	res := callTool(t, cs, "send_message", map[string]any{"chat_id": 0, "chat_type": "private", "text": "hi"})
	expectToolError(t, res, "chat_id is required")

	res = callTool(t, cs, "send_message", map[string]any{"chat_id": 7, "chat_type": "private", "text": "   "})
	expectToolError(t, res, "text is required")

	res = callTool(t, cs, "send_message", map[string]any{"chat_id": 7, "chat_type": "dm", "text": "hi"})
	expectToolError(t, res, "chat_type")

	if fake.sendCalled {
		t.Fatal("SendMessage was called despite invalid input")
	}
}

func TestSendMessage_BackendError(t *testing.T) {
	cs := newSession(t, &fakeTelegram{sendErr: errors.New("boom")})
	res := callTool(t, cs, "send_message", map[string]any{"chat_id": 7, "chat_type": "private", "text": "hi"})
	expectToolError(t, res, "boom")
}
