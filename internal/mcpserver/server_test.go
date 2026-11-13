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

	getUserCalled bool
	getUserParams tg.GetUserParams
	getUserResult *tg.UserInfo
	getUserErr    error

	partsCalled bool
	partsParams tg.ListParticipantsParams
	partsResult *tg.ListParticipantsResult
	partsErr    error

	downloadCalled bool
	downloadParams tg.DownloadMediaParams
	downloadResult *tg.DownloadMediaResult
	downloadErr    error

	sendFileCalled bool
	sendFileParams tg.SendFileParams
	sendFileResult *tg.SendFileResult
	sendFileErr    error
}

func (f *fakeTelegram) SendFile(_ context.Context, p tg.SendFileParams) (*tg.SendFileResult, error) {
	f.sendFileCalled = true
	f.sendFileParams = p
	if f.sendFileErr != nil {
		return nil, f.sendFileErr
	}
	return f.sendFileResult, nil
}

func (f *fakeTelegram) DownloadMedia(_ context.Context, p tg.DownloadMediaParams) (*tg.DownloadMediaResult, error) {
	f.downloadCalled = true
	f.downloadParams = p
	if f.downloadErr != nil {
		return nil, f.downloadErr
	}
	return f.downloadResult, nil
}

func (f *fakeTelegram) ListParticipants(_ context.Context, p tg.ListParticipantsParams) (*tg.ListParticipantsResult, error) {
	f.partsCalled = true
	f.partsParams = p
	if f.partsErr != nil {
		return nil, f.partsErr
	}
	return f.partsResult, nil
}

func (f *fakeTelegram) GetUser(_ context.Context, p tg.GetUserParams) (*tg.UserInfo, error) {
	f.getUserCalled = true
	f.getUserParams = p
	if f.getUserErr != nil {
		return nil, f.getUserErr
	}
	return f.getUserResult, nil
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
	want := map[string]bool{"list_chats": true, "read_chat": true, "send_message": false, "get_user": true, "list_participants": true, "download_media": true, "send_file": false}
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

func TestDownloadMedia_PassesParams(t *testing.T) {
	fake := &fakeTelegram{downloadResult: &tg.DownloadMediaResult{Path: "/d/7_10.ogg", Kind: "voice", MimeType: "audio/ogg", Size: 3, Duration: 7}}
	cs := newSession(t, fake)

	res := callTool(t, cs, "download_media", map[string]any{"chat_id": 7, "chat_type": "private", "message_id": 10})
	if res.IsError {
		t.Fatalf("IsError = true: %s", resultText(t, res))
	}
	want := tg.DownloadMediaParams{ChatID: 7, ChatType: tg.ChatPrivate, MessageID: 10}
	if fake.downloadParams != want {
		t.Fatalf("params = %+v, want %+v", fake.downloadParams, want)
	}
	var got tg.DownloadMediaResult
	if err := json.Unmarshal([]byte(resultText(t, res)), &got); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if got != *fake.downloadResult {
		t.Fatalf("result = %+v, want %+v", got, *fake.downloadResult)
	}
}

func TestDownloadMedia_Validation(t *testing.T) {
	fake := &fakeTelegram{}
	cs := newSession(t, fake)

	res := callTool(t, cs, "download_media", map[string]any{"chat_id": 0, "chat_type": "private", "message_id": 10})
	expectToolError(t, res, "chat_id is required")

	res = callTool(t, cs, "download_media", map[string]any{"chat_id": 7, "chat_type": "dm", "message_id": 10})
	expectToolError(t, res, "chat_type")

	for _, id := range []int{0, -1} {
		res = callTool(t, cs, "download_media", map[string]any{"chat_id": 7, "chat_type": "private", "message_id": id})
		expectToolError(t, res, "message_id must be positive")
	}

	if fake.downloadCalled {
		t.Fatal("DownloadMedia was called despite invalid input")
	}
}

func TestDownloadMedia_BackendError(t *testing.T) {
	cs := newSession(t, &fakeTelegram{downloadErr: errors.New("boom")})
	res := callTool(t, cs, "download_media", map[string]any{"chat_id": 7, "chat_type": "private", "message_id": 10})
	expectToolError(t, res, "boom")
}

func TestGetUser_ByID(t *testing.T) {
	fake := &fakeTelegram{getUserResult: &tg.UserInfo{ID: 7, FirstName: "Ann", Status: "online"}}
	cs := newSession(t, fake)

	res := callTool(t, cs, "get_user", map[string]any{"user_id": 7})
	if res.IsError {
		t.Fatalf("IsError = true: %s", resultText(t, res))
	}
	if fake.getUserParams != (tg.GetUserParams{UserID: 7}) {
		t.Fatalf("params = %+v, want UserID 7", fake.getUserParams)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(resultText(t, res)), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got["id"] != float64(7) || got["first_name"] != "Ann" {
		t.Fatalf("result = %v", got)
	}
}

func TestGetUser_UsernameStripped(t *testing.T) {
	for in, want := range map[string]string{"@ann": "ann", "  ann  ": "ann", " @Ann ": "Ann"} {
		fake := &fakeTelegram{getUserResult: &tg.UserInfo{ID: 7}}
		cs := newSession(t, fake)
		res := callTool(t, cs, "get_user", map[string]any{"username": in})
		if res.IsError {
			t.Fatalf("%q: IsError = true: %s", in, resultText(t, res))
		}
		if fake.getUserParams != (tg.GetUserParams{Username: want}) {
			t.Fatalf("%q: params = %+v, want Username %q", in, fake.getUserParams, want)
		}
	}
}

func TestGetUser_Validation(t *testing.T) {
	tests := []struct {
		args map[string]any
		want string
	}{
		{map[string]any{}, "pass exactly one of user_id or username"},
		{map[string]any{"user_id": 7, "username": "ann"}, "pass exactly one of user_id or username"},
		{map[string]any{"username": "@"}, "username is empty"},
		{map[string]any{"username": "   "}, "username is empty"},
		{map[string]any{"username": " @ "}, "username is empty"},
		{map[string]any{"user_id": -5}, "user_id must be positive"},
	}
	for _, tt := range tests {
		fake := &fakeTelegram{}
		cs := newSession(t, fake)
		expectToolError(t, callTool(t, cs, "get_user", tt.args), tt.want)
		if fake.getUserCalled {
			t.Fatalf("%v: backend called", tt.args)
		}
	}
}

func TestGetUser_BackendError(t *testing.T) {
	fake := &fakeTelegram{getUserErr: tg.ErrUsernameNotFound}
	cs := newSession(t, fake)
	expectToolError(t, callTool(t, cs, "get_user", map[string]any{"username": "nobody"}), "username not found")
}

func TestListParticipants_PassesParams(t *testing.T) {
	total := 3
	fake := &fakeTelegram{partsResult: &tg.ListParticipantsResult{
		Source:       tg.SourceMembers,
		Participants: []tg.Participant{{ID: 7, FirstName: "Ann", Role: "member"}},
		Total:        &total,
	}}
	cs := newSession(t, fake)
	res := callTool(t, cs, "list_participants", map[string]any{
		"chat_id": 5, "chat_type": "supergroup", "query": " ann ", "filter": "bots", "limit": 10, "cursor": "c",
	})
	if res.IsError {
		t.Fatalf("IsError = true: %s", resultText(t, res))
	}
	want := tg.ListParticipantsParams{ChatID: 5, ChatType: tg.ChatSupergroup, Query: "ann", Filter: tg.FilterBots, Limit: 10, Cursor: "c"}
	if fake.partsParams != want {
		t.Fatalf("params = %+v, want %+v", fake.partsParams, want)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(resultText(t, res)), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got["source"] != "members" || got["total"] != float64(3) || got["next_cursor"] != nil {
		t.Fatalf("result = %v", got)
	}
}

func TestListParticipants_Defaults(t *testing.T) {
	fake := &fakeTelegram{partsResult: &tg.ListParticipantsResult{Source: tg.SourceRecentSenders, Participants: []tg.Participant{}}}
	cs := newSession(t, fake)
	res := callTool(t, cs, "list_participants", map[string]any{"chat_id": 5, "chat_type": "group"})
	if res.IsError {
		t.Fatalf("IsError = true: %s", resultText(t, res))
	}
	if fake.partsParams.Filter != tg.FilterAll || fake.partsParams.Limit != 50 || fake.partsParams.ChatType != tg.ChatGroup {
		t.Fatalf("params = %+v", fake.partsParams)
	}
}

func TestListParticipants_Validation(t *testing.T) {
	for _, tc := range []struct {
		args map[string]any
		want string
	}{
		{map[string]any{"chat_id": 0, "chat_type": "group"}, "chat_id is required"},
		{map[string]any{"chat_id": 5, "chat_type": "private"}, "list_participants supports group and supergroup only"},
		{map[string]any{"chat_id": 5, "chat_type": "channel"}, "list_participants supports group and supergroup only"},
		{map[string]any{"chat_id": 5, "chat_type": "bogus"}, "chat_type must be one of"},
		{map[string]any{"chat_id": 5, "chat_type": "group", "filter": "kicked"}, "filter must be all, admins or bots"},
		{map[string]any{"chat_id": 5, "chat_type": "group", "limit": 101}, "limit must be between 1 and 100"},
	} {
		fake := &fakeTelegram{}
		cs := newSession(t, fake)
		expectToolError(t, callTool(t, cs, "list_participants", tc.args), tc.want)
		if fake.partsCalled {
			t.Fatalf("%v: backend called", tc.args)
		}
	}
}

func TestListParticipants_BackendError(t *testing.T) {
	cs := newSession(t, &fakeTelegram{partsErr: tg.ErrAdminsHidden})
	res := callTool(t, cs, "list_participants", map[string]any{"chat_id": 5, "chat_type": "supergroup", "filter": "admins"})
	expectToolError(t, res, "the group hides its admin list from members")
}

func TestSendFile_Passthrough(t *testing.T) {
	fake := &fakeTelegram{sendFileResult: &tg.SendFileResult{ID: 42, Date: 500}}
	cs := newSession(t, fake)

	res := callTool(t, cs, "send_file", map[string]any{"chat_id": 7, "chat_type": "private", "path": "/x/a.ogg", "kind": "voice", "reply_to": 3})
	if res.IsError {
		t.Fatalf("IsError = true: %s", resultText(t, res))
	}
	want := tg.SendFileParams{ChatID: 7, ChatType: tg.ChatPrivate, Path: "/x/a.ogg", Kind: tg.KindVoice, ReplyTo: 3}
	if fake.sendFileParams != want {
		t.Fatalf("params = %+v, want %+v", fake.sendFileParams, want)
	}
	var got tg.SendFileResult
	if err := json.Unmarshal([]byte(resultText(t, res)), &got); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if got != *fake.sendFileResult {
		t.Fatalf("result = %+v, want %+v", got, *fake.sendFileResult)
	}
}

func TestSendFile_Validation(t *testing.T) {
	base := func() map[string]any {
		return map[string]any{"chat_id": 7, "chat_type": "private", "path": "/x/a.mp4", "kind": "video"}
	}
	cases := []struct {
		name string
		mod  func(m map[string]any)
		want string
	}{
		{"chat_id zero", func(m map[string]any) { m["chat_id"] = 0 }, "chat_id is required"},
		{"bad chat_type", func(m map[string]any) { m["chat_type"] = "x" }, "chat_type must be one of"},
		{"empty path", func(m map[string]any) { m["path"] = "" }, "path is required"},
		{"relative path", func(m map[string]any) { m["path"] = "a.ogg" }, "path must be an absolute path"},
		{"bad kind", func(m map[string]any) { m["kind"] = "gif" }, "kind must be one of photo, document, audio, voice, video, video_note"},
		{"video_note caption", func(m map[string]any) { m["kind"] = "video_note"; m["caption"] = "hi" }, "video_note cannot have a caption"},
		{"negative reply_to", func(m map[string]any) { m["reply_to"] = -1 }, "reply_to must not be negative"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fake := &fakeTelegram{}
			cs := newSession(t, fake)
			m := base()
			c.mod(m)
			expectToolError(t, callTool(t, cs, "send_file", m), c.want)
			if fake.sendFileCalled {
				t.Fatal("SendFile was called despite invalid input")
			}
		})
	}
}

func TestSendFile_WhitespaceCaptionVideoNote(t *testing.T) {
	fake := &fakeTelegram{sendFileResult: &tg.SendFileResult{ID: 1, Date: 2}}
	cs := newSession(t, fake)
	res := callTool(t, cs, "send_file", map[string]any{"chat_id": 7, "chat_type": "private", "path": "/x/a.mp4", "kind": "video_note", "caption": "  "})
	if res.IsError {
		t.Fatalf("IsError = true: %s", resultText(t, res))
	}
	if fake.sendFileParams.Caption != "" {
		t.Fatalf("Caption = %q, want empty", fake.sendFileParams.Caption)
	}
}

func TestSendFile_BackendError(t *testing.T) {
	cs := newSession(t, &fakeTelegram{sendFileErr: errors.New("boom")})
	res := callTool(t, cs, "send_file", map[string]any{"chat_id": 7, "chat_type": "private", "path": "/x/a.jpg", "kind": "photo"})
	expectToolError(t, res, "boom")
}

func TestNew_ListsSendFile(t *testing.T) {
	cs := newSession(t, &fakeTelegram{})
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	for _, tool := range res.Tools {
		if tool.Name == "send_file" {
			if tool.Annotations != nil && tool.Annotations.ReadOnlyHint {
				t.Fatal("send_file must not be read-only")
			}
			return
		}
	}
	t.Fatal("send_file not listed")
}
