package tg

import "errors"

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
	Pinned          bool     `json:"pinned,omitempty"`
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

type SendMessageParams struct {
	ChatID   int64
	ChatType ChatType
	Text     string // Markdown
	ReplyTo  int    // message id to reply to; 0 for none
}

type SendMessageResult struct {
	ID   int   `json:"id"`
	Date int64 `json:"date"`
}

type DownloadMediaParams struct {
	ChatID    int64
	ChatType  ChatType
	MessageID int
}

type DownloadMediaResult struct {
	Path     string `json:"path"`                // absolute
	Kind     string `json:"kind"`                // voice, audio, video, video_note, photo, document, sticker, gif
	MimeType string `json:"mime_type"`
	Size     int64  `json:"size"`                // bytes on disk
	FileName string `json:"file_name,omitempty"` // original name, if the document has one
	Duration int    `json:"duration,omitempty"`  // seconds
}

type GetUserParams struct {
	UserID   int64  // id of a private chat; 0 when Username is set
	Username string // without @; "" when UserID is set
}

// UserInfo is what Telegram shows the owner about one user. Fields Telegram
// hides are left empty and omitted from JSON.
type UserInfo struct {
	ID        int64     `json:"id"`
	FirstName string    `json:"first_name"`
	LastName  string    `json:"last_name,omitempty"`
	Username  string    `json:"username,omitempty"`
	Usernames []string  `json:"usernames,omitempty"`
	Phone     string    `json:"phone,omitempty"`
	Bio       string    `json:"bio,omitempty"`
	Birthday  *Birthday `json:"birthday,omitempty"`
	Note      string    `json:"note,omitempty"`
	LangCode  string    `json:"lang_code,omitempty"`

	Status   string `json:"status"`
	LastSeen int64  `json:"last_seen,omitempty"`

	// Relation to the owner. Always present.
	Contact          bool `json:"contact"`
	MutualContact    bool `json:"mutual_contact"`
	CloseFriend      bool `json:"close_friend"`
	Blocked          bool `json:"blocked"`
	CommonChatsCount int  `json:"common_chats_count"`

	// Account flags. Present only when true.
	Bot                bool     `json:"bot,omitempty"`
	Premium            bool     `json:"premium,omitempty"`
	Verified           bool     `json:"verified,omitempty"`
	Scam               bool     `json:"scam,omitempty"`
	Fake               bool     `json:"fake,omitempty"`
	Support            bool     `json:"support,omitempty"`
	Deleted            bool     `json:"deleted,omitempty"`
	Restricted         bool     `json:"restricted,omitempty"`
	RestrictionReasons []string `json:"restriction_reasons,omitempty"`

	HasPhoto          bool      `json:"has_photo"`
	PersonalChannelID int64     `json:"personal_channel_id,omitempty"`
	Business          *Business `json:"business,omitempty"`
	BotDescription    string    `json:"bot_description,omitempty"`
}

type Birthday struct {
	Day   int `json:"day"`
	Month int `json:"month"`
	Year  int `json:"year,omitempty"`
}

type Business struct {
	Address   string     `json:"address,omitempty"`
	Intro     string     `json:"intro,omitempty"`
	WorkHours *WorkHours `json:"work_hours,omitempty"`
}

type WorkHours struct {
	Timezone string   `json:"timezone"` // e.g. "Europe/Berlin"
	Open     []string `json:"open"`     // e.g. "Mon 09:00-18:00"
}

type ParticipantSource string

const (
	SourceMembers       ParticipantSource = "members"
	SourceRecentSenders ParticipantSource = "recent_senders"
)

type ParticipantFilter string

const (
	FilterAll    ParticipantFilter = "all"
	FilterAdmins ParticipantFilter = "admins"
	FilterBots   ParticipantFilter = "bots"
)

type ListParticipantsParams struct {
	ChatID   int64
	ChatType ChatType // group or supergroup
	Query    string
	Filter   ParticipantFilter
	Limit    int
	Cursor   string
}

type Participant struct {
	ID              int64  `json:"id"`
	FirstName       string `json:"first_name"`
	LastName        string `json:"last_name,omitempty"`
	Username        string `json:"username,omitempty"`
	Bot             bool   `json:"bot,omitempty"`
	Deleted         bool   `json:"deleted,omitempty"`
	Role            string `json:"role,omitempty"`              // creator | admin | member; members source only
	JoinedAt        int64  `json:"joined_at,omitempty"`         // unix seconds; members source only, when known
	LastMessageDate int64  `json:"last_message_date,omitempty"` // unix seconds; recent_senders only
}

type ListParticipantsResult struct {
	Source       ParticipantSource `json:"source"`
	Participants []Participant     `json:"participants"`
	Total        *int              `json:"total"`       // member count Telegram reports; null for recent_senders
	NextCursor   *string           `json:"next_cursor"` // null on the last page
}

// FileKind is how send_file sends a file.
type FileKind string

const (
	KindPhoto     FileKind = "photo"
	KindDocument  FileKind = "document"
	KindAudio     FileKind = "audio"
	KindVoice     FileKind = "voice"
	KindVideo     FileKind = "video"
	KindVideoNote FileKind = "video_note"
)

// ParseFileKind returns the kind named by s.
func ParseFileKind(s string) (FileKind, error) {
	switch k := FileKind(s); k {
	case KindPhoto, KindDocument, KindAudio, KindVoice, KindVideo, KindVideoNote:
		return k, nil
	}
	return "", errors.New("kind must be one of photo, document, audio, voice, video, video_note")
}
