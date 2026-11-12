# `get_user` tool — Design

Date: 2026-11-12
Status: draft, awaiting approval

## Intent

The owner keeps a "second brain" fed by Telegram. Chat text alone misses
facts about the people: birthday, phone number, bio, how they relate to
the owner. `get_user` returns everything Telegram shows the owner about
one user, in one curated structure.

## Goals

- Look up one user by `user_id` (the `chat_id` of a private chat from
  `list_chats`) or by `username`.
- Return every person-related field Telegram exposes: profile, birthday,
  phone, bio, status, relation to the owner, account flags, business info.
- Read-only. No change to the owner's account.

## Non-goals

- Looking up senders of group messages. `read_chat` does not return sender
  ids, and "min" users in large groups need `InputUserFromMessage`. This can
  be added later.
- Looking up the owner's own profile (`me`).
- Downloading the profile photo.
- Cosmetic and client-setting fields: chat theme, wallpaper, name colors,
  emoji status, notification settings, stories, star ratings, gifts,
  sponsored-ads flags, bot admin rights, TTL period.
- Raw TL dump.
- Batch lookup of many users in one call.

## Anti-goals

- Getting the account rate-limited. `contacts.resolveUsername` is
  rate-limited by Telegram; flood waits are obeyed by the existing
  middleware (see main design doc).
- Leaking personal data: user data (phone especially) is never written to
  logs. It only goes into the tool result.

## Decisions made by the owner

- Lookup by `user_id` and by `username`. Group senders are out of scope.
- Curated output, not every raw field.

## MCP tool

### `get_user`

Annotations: `ReadOnlyHint: true`.

Description (for the model): "Get all profile details Telegram shows for one
user: name, username, phone, bio, birthday, status, relation to you, account
flags, business info. Pass user_id (chat_id of a private chat from
list_chats) or username. Fields Telegram hides are omitted."

Input:

```go
type GetUserInput struct {
    UserID   int64  `json:"user_id,omitempty"  jsonschema:"chat_id of a private chat from list_chats"`
    Username string `json:"username,omitempty" jsonschema:"Telegram username, with or without @"`
}
```

Validation (in `mcpserver`):

- Exactly one of `user_id` and `username` is set. Otherwise error
  `"pass exactly one of user_id or username"`.
- `username` is trimmed and a leading `@` is removed. Empty after that is
  an error.

Output: `UserInfo` (below).

### Output structure

Lives in `internal/tg/types.go`.

```go
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
    Bot        bool `json:"bot,omitempty"`
    Premium    bool `json:"premium,omitempty"`
    Verified   bool `json:"verified,omitempty"`
    Scam       bool `json:"scam,omitempty"`
    Fake       bool `json:"fake,omitempty"`
    Support    bool `json:"support,omitempty"`
    Deleted    bool `json:"deleted,omitempty"`
    Restricted bool `json:"restricted,omitempty"`
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
```

Field mapping (gotd `tg.User` = U, `tg.UserFull` = F):

| Field | Source | Rule |
|---|---|---|
| `id` | U.ID | |
| `first_name`, `last_name` | U.FirstName, U.LastName | |
| `username` | U.Username, else first active entry of U.Usernames | primary username |
| `usernames` | U.Usernames | other active usernames, primary excluded |
| `phone` | U.Phone | as given by Telegram; empty if hidden |
| `bio` | F.About | |
| `birthday` | F.Birthday | nil if F has no birthday; `year` omitted if unset |
| `note` | F.Note.Text | the owner's private note on the contact |
| `lang_code` | U.LangCode | |
| `status` | U.Status | `UserStatusOnline` → `online`; `UserStatusOffline` → `offline`; `UserStatusRecently` → `recently`; `UserStatusLastWeek` → `last_week`; `UserStatusLastMonth` → `last_month`; empty or missing → `unknown` |
| `last_seen` | UserStatusOffline.WasOnline | only for `offline` |
| `contact`, `mutual_contact`, `close_friend` | U flags | |
| `blocked` | F.Blocked | |
| `common_chats_count` | F.CommonChatsCount | |
| account flags | U flags | |
| `restriction_reasons` | U.RestrictionReason | each as `"platform: reason: text"` |
| `has_photo` | U.Photo | true if `*tg.UserProfilePhoto` |
| `personal_channel_id` | F.PersonalChannelID | |
| `business.address` | F.BusinessLocation.Address | |
| `business.intro` | F.BusinessIntro | `title` and `description` joined by `\n`, empty parts dropped |
| `business.work_hours` | F.BusinessWorkHours | see below |
| `bot_description` | F.BotInfo.Description | bots only |

`business` is nil when all three parts are empty.

Work hours: Telegram gives intervals as minutes from Monday 00:00 in the
user's timezone (week = 0..10080). Each interval becomes one string
`"<Day> HH:MM-HH:MM"`. An interval that crosses midnight is written with
the start day and an end time past midnight, e.g. `"Fri 22:00-02:00"`.

## Lookup flow

New file `internal/tg/users.go`, method
`func (s *Service) GetUser(ctx, GetUserParams) (*UserInfo, error)`.

`GetUserParams{UserID int64; Username string}`.

1. Get an `InputUser`:
   - By `user_id`: `s.resolvePeer(ctx, ChatPrivate, id)`. It uses the peer
     cache and, on a miss, scans dialogs (existing code). Convert the
     `InputPeerUser` to `InputUser`. On `ErrUnknownChat`, return
     `"unknown user; pass a chat_id of a private chat from list_chats, or a username"`.
   - By `username`: call `contacts.resolveUsername`. Store `Users`/`Chats`
     from the response in the peer cache and save it. If the resolved peer
     is not a user, return `"username belongs to a group or channel, not a user"`.
     On `USERNAME_NOT_OCCUPIED` / `USERNAME_INVALID`, return
     `"username not found"`.
2. Call `users.getFullUser`. Store `Users`/`Chats` in the peer cache and
   save it (a save failure is a warning, as in `read_chat`).
3. Find the `*tg.User` with the requested id in the response `Users`.
   Build `UserInfo` from it and `FullUser`.
4. A deleted account returns what Telegram gives, with `deleted: true`.

The `Telegram` interface in `mcpserver` gets
`GetUser(ctx, tg.GetUserParams) (*tg.UserInfo, error)`.

## Error handling

Same as the existing tools: errors return as tool errors with a short text.
Flood waits are handled by the existing middleware.

## Testing

- `internal/tg/users_test.go` with `tgmock`, like `dialogs_test.go`:
  - by `user_id` with cache hit: one `users.getFullUser` call, mapping
    correct for a full set of fields.
  - by `user_id` with cache miss: dialogs scan, then `users.getFullUser`.
  - by `username`: `contacts.resolveUsername` then `users.getFullUser`;
    peer cache gets the user.
  - username resolves to a channel → error.
  - username not occupied → error.
  - hidden fields (no phone, no birthday) are omitted in JSON.
  - each status type maps correctly.
  - work hours formatting, including an interval across midnight.
- `internal/mcpserver/server_test.go`: input validation (none set, both
  set, `@name` stripped, blank username).

## Docs

- README: add `get_user` to the tool list.

## Left to the builder

- Helper names and file split inside `internal/tg`.
- Exact wording of the tool description, within the meaning above.
