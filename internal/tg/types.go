package tg

type ChatType string

const (
	ChatPrivate    ChatType = "private"
	ChatGroup      ChatType = "group"
	ChatSupergroup ChatType = "supergroup"
	ChatChannel    ChatType = "channel"
)

type Folder string

const (
	FolderMain    Folder = "main"
	FolderArchive Folder = "archive"
)

const (
	DefaultLimit = 50
	MaxLimit     = 100
)

type Chat struct {
	ID              int64    `json:"id"`
	Type            ChatType `json:"type"`
	Title           string   `json:"title"`
	Username        string   `json:"username,omitempty"`
	LastMessageDate int64    `json:"last_message_date"`
	UnreadCount     int      `json:"unread_count"`
	Folder          Folder   `json:"folder"`
}

type Message struct {
	ID   int    `json:"id"`
	Date int64  `json:"date"`
	From string `json:"from"`
	Text string `json:"text"`
}

type ListChatsParams struct {
	Folder Folder
	Limit  int
	Cursor string
}

type ListChatsResult struct {
	Chats      []Chat  `json:"chats"`
	NextCursor *string `json:"next_cursor"`
}

type ReadChatParams struct {
	ChatID    int64
	ChatType  ChatType
	Limit     int
	BeforeID  int
	SinceDate int64
}

type ReadChatResult struct {
	Messages     []Message `json:"messages"`
	NextBeforeID *int      `json:"next_before_id"`
}
