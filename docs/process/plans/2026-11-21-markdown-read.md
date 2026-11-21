# Markdown text in `read_chat` Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `read_chat` returns every message `text` as Markdown: entities of ordinary messages and the blocks of rich posts.

**Architecture:** Two pure renderers in `internal/tg` share one marker table: `entities.go` (entities → Markdown, boundary sweep) and `richtext.go` (rich blocks → Markdown, recursive switch). `RenderText` picks one. `ReadChat` fetches the full post with `messages.getRichMessage` when a rich post is partial.

**Tech Stack:** Go 1.25, gotd v0.163.0 (`github.com/gotd/td/tg`), gotd `tgmock` in tests.

**Spec:** `docs/process/specs/2026-11-21-markdown-read-design.md`

## Global Constraints

- Work only in the worktree `.claude/worktrees/markdown-read`, branch `feat/markdown-read`.
- No escaping of `*`, `_`, `[` etc. in source text.
- No lost text: every visible character of the source appears in the output, in order.
- No panic and no error from rendering, for any block, rich text or entity type, known or new.
- Shared markers: bold `**`, italic `*`, strike `~~`, code `` ` ``, spoiler `||`, marked `==`, link `[text](url)`, user `[text](tg://user?id=N)`, underline none.
- A URL with `)` or a space is written as `[text](<url>)`.
- Entity offsets and lengths are UTF-16 code units.
- `go test ./...` and `go vet ./...` pass before every commit.

## Review Focus

1. An entity whose UTF-16 range splits a surrogate pair (half an emoji). Expect: no panic, the emoji appears whole, once.
2. Inline code whose text contains a backtick. Expect: a longer backtick run, for example ``` `` a`b `` ```.
3. A `Pre` entity in the middle of a line, with text after it on the same line. Expect: the fence opens and closes on its own lines and the following text is kept.
4. A rich post with no blocks, or with only `Anchor` and `Divider` blocks. Expect: `""` or `---`, no panic, no stray blank lines.
5. A list item that holds blocks (`PageListItemBlocks`) with two paragraphs. Expect: the second paragraph is indented under the item, not a new top-level paragraph.

Each item has a test in the task that owns the code.

---

### Task 1: Shared markers and entities renderer

**Files:**
- Create: `internal/tg/entities.go`
- Test: `internal/tg/entities_test.go`

**Interfaces:**
- Produces:
  - `func RenderEntities(text string, ents []tgapi.MessageEntityClass) string`
  - `type marker struct{ open, close string }` and `func styleMarker(kind styleKind) marker`, with `styleKind` constants `styleBold, styleItalic, styleStrike, styleCode, styleSpoiler, styleMarked`.
  - `func linkMarker(url string) marker`: open `[`, close `](url)` or `](<url>)`.
  - `func userURL(id int64) string`: returns `tg://user?id=N`.
  - `func codeSpan(s string) string`: wraps `s` in a backtick run one longer than the longest run inside `s`. It adds a space inside the run if `s` starts or ends with a backtick.
  - `func fence(lang, body string) string`: a fenced block. It uses a fence longer than any backtick run at line start inside `body`.

- [ ] **Step 1: Write the failing tests** (table-driven `TestRenderEntities`, each row `{name, text, ents, want}`)

```go
{"no entities", "a_b *c* [d]", nil, "a_b *c* [d]"},
{"bold", "hi there", ents(&tgapi.MessageEntityBold{Offset: 3, Length: 5}), "hi **there**"},
{"italic", "x", ents(&tgapi.MessageEntityItalic{Length: 1}), "*x*"},
{"strike", "x", ents(&tgapi.MessageEntityStrike{Length: 1}), "~~x~~"},
{"spoiler", "x", ents(&tgapi.MessageEntitySpoiler{Length: 1}), "||x||"},
{"code", "run ls", ents(&tgapi.MessageEntityCode{Offset: 4, Length: 2}), "run `ls`"},
{"code with backtick", "a`b", ents(&tgapi.MessageEntityCode{Length: 3}), "``a`b``"},
{"text url", "see docs", ents(&tgapi.MessageEntityTextURL{Offset: 4, Length: 4, URL: "https://x.io"}), "see [docs](https://x.io)"},
{"url with paren", "w", ents(&tgapi.MessageEntityTextURL{Length: 1, URL: "https://x/(a)"}), "[w](<https://x/(a)>)"},
{"mention name", "Ann", ents(&tgapi.MessageEntityMentionName{Length: 3, UserID: 42}), "[Ann](tg://user?id=42)"},
{"underline", "u", ents(&tgapi.MessageEntityUnderline{Length: 1}), "u"},
{"auto types plain", "@ann #tag https://x.io", ents(&tgapi.MessageEntityMention{Length: 4}, &tgapi.MessageEntityHashtag{Offset: 5, Length: 4}, &tgapi.MessageEntityURL{Offset: 10, Length: 12}), "@ann #tag https://x.io"},
{"utf16 offsets", "😀 ok 😀b", ents(&tgapi.MessageEntityBold{Offset: 3, Length: 2}, &tgapi.MessageEntityItalic{Offset: 6, Length: 3}), "😀 **ok** *😀b*"},
{"overlap", "0123456789abcde", ents(&tgapi.MessageEntityBold{Length: 10}, &tgapi.MessageEntityTextURL{Offset: 5, Length: 10, URL: "u"}), "**01234[56789](u)**[abcde](u)"},
{"no markers in code", "a b", ents(&tgapi.MessageEntityCode{Length: 3}, &tgapi.MessageEntityBold{Length: 3}), "`a b`"},
{"pre lang", "x:\nfmt.Println()\ny", ents(&tgapi.MessageEntityPre{Offset: 3, Length: 13, Language: "go"}), "x:\n```go\nfmt.Println()\n```\ny"},
{"pre mid line", "a code b", ents(&tgapi.MessageEntityPre{Offset: 2, Length: 4}), "a \n```\ncode\n```\n b"},
{"blockquote", "q1\nq2", ents(&tgapi.MessageEntityBlockquote{Length: 5}), "> q1\n> q2"},
{"edge whitespace", "a  b c", ents(&tgapi.MessageEntityBold{Offset: 1, Length: 3}), "a  **b** c"},
{"unknown type", "abc", ents(&tgapi.MessageEntityUnknown{Length: 3}), "abc"},
{"out of range", "abc", ents(&tgapi.MessageEntityBold{Offset: 2, Length: 50}), "ab**c**"},
{"empty range", "abc", ents(&tgapi.MessageEntityBold{Offset: 1}), "abc"},
```

Add `TestRenderEntities_SplitSurrogate` (Review Focus 1): text `"😀x"`, entity `Bold{Offset: 1, Length: 2}`. Assert no panic, `strings.Count(got, "😀") == 1`, and `stripMarkers(got) == "😀x"`.

Add `TestRenderEntities_NoLostText`. It runs every row above and checks that `stripMarkers(got)` (removes `**`, `*`, `~~`, `` ` ``, `||`, `> `, fence lines, `[`, `](…)`) contains the input runes in order.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tg -run TestRenderEntities`
Expected: FAIL, `undefined: RenderEntities`.

- [ ] **Step 3: Implement `RenderEntities` and the marker helpers in `internal/tg/entities.go`**

Algorithm (the spec leaves it to the builder; this is the decision):

```
u := utf16.Encode([]rune(text)); clamp each range to [0, len(u)], drop empty ones,
move range edges that split a surrogate pair outward to the pair boundary.
Map Pre/Blockquote to block spans; map the others to inline spans (style or link).
Drop inline spans inside a Code or Pre range.
For inline spans: trim edge whitespace out of the range.
Cut u at every span start/end. For each segment, the active spans
are sorted by (start asc, end desc, kind). Keep a stack of open spans:
close from the top down to the longest common prefix with the new active list,
then open the rest. Code content is written with codeSpan, not markers.
Write Pre with fence(); write Blockquote by prefixing "> " to each line of its rendered content.
Insert "\n" before or after a block span when it is not at a line edge.
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tg -run TestRenderEntities -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
go vet ./... && go test ./...
git add internal/tg/entities.go internal/tg/entities_test.go
git commit -m "feat(tg): render message entities as Markdown"
```

### Task 2: Rich post renderer

**Files:**
- Create: `internal/tg/richtext.go`
- Test: `internal/tg/richtext_test.go`

**Interfaces:**
- Consumes from Task 1: `styleMarker`, `linkMarker`, `userURL`, `codeSpan`, `fence`.
- Produces: `func RenderRich(rm tgapi.RichMessage) string`.

- [ ] **Step 1: Write the failing tests**

Test helpers in the test file: `p(s string) *tgapi.TextPlain`, `para(t tgapi.RichTextClass) *tgapi.PageBlockParagraph`.

`TestRenderRich_Golden`: one `RichMessage` with these blocks, in order. Expect exactly this output:

| Block | Output line(s) |
| --- | --- |
| `PageBlockHeading1{Text: p("Title")}` | `# Title` |
| `PageBlockHeading2{Text: p("Sub")}` | `## Sub` |
| `PageBlockHeading4{Text: p("Deep")}` | `#### Deep` |
| `para(TextConcat{p("See "), TextURL{Text: p("post"), URL: "https://t.me/c/29"}, p(".")})` | `See [post](https://t.me/c/29).` |
| `para(TextBold{TextItalic{p("bi")}})` | `***bi***` |
| `PageBlockList{Items: [PageListItemText{p("a")}, PageListItemText{p("b")}]}` | `- a` / `- b` |
| `PageBlockOrderedList{Items: [PageListOrderedItemText{Num: "1", Text: p("x")}, …Num "2", p("y")]}` | `1. x` / `2. y` |
| `PageBlockBlockquote{Text: p("q"), Caption: p("Ann")}` | `> q` / `> — Ann` |
| `PageBlockPreformatted{Text: p("a := 1"), Language: "go"}` | fenced `go` block |
| `PageBlockTable` with rows `[h1 h2]`, `[a\|b, c]` | `\| h1 \| h2 \|` / `\| --- \| --- \|` / `\| a\|b \| c \|` (pipe in cell escaped) |
| `PageBlockDivider{}` | `---` |
| `PageBlockAnchor{Name: "x"}` | nothing |

Blocks are joined by one blank line, and the output has no leading or trailing blank lines.

Other tests (one `want` each):
- `TestRenderRich_RichText`: `TextFixed{TextBold{p("x")}}` → `` `x` ``; `TextStrike` → `~~`; `TextSpoiler` → `||`; `TextMarked` → `==`; `TextEmail{p("m"), Email: "a@b"}` → `[m](mailto:a@b)`; `TextPhone{p("t"), Phone: "+1"}` → `[t](tel:+1)`; `TextMentionName{p("Ann"), UserID: 42}` → `[Ann](tg://user?id=42)`; `TextMath{Text:"x^2"}` → `$x^2$`; `TextImage{}` → `[image]`; `TextUnderline{p("u")}` → `u`; `TextEmpty{}` → `""`.
- `TestRenderRich_Media`: `PageBlockPhoto{Caption: PageCaption{Text: p("cap")}}` → `[photo] cap`; caption with `Credit: p("Bob")` → `[photo] cap — Bob`; photo with an empty caption → `[photo]`.
- `TestRenderRich_EmbedPostKeepsText`: `PageBlockEmbedPost{URL: "u", Blocks: [para(p("inner"))]}` → `[embed u]\n\n> inner`.
- `TestRenderRich_Collage`: `PageBlockCollage{Items: [PageBlockPhoto{Caption: {Text: p("one")}}], Caption: {Text: p("all")}}` → `[album] all\n\n[photo] one`.
- `TestRenderRich_Unknown`: `PageBlockUnsupported{}` → `""`; `PageBlockKicker{Text: p("k")}` → `k`; a `PageBlockButtonRow` does not panic.
- `TestRenderRich_Empty` (Review Focus 4): no blocks → `""`; `[Anchor, Divider, Anchor]` → `---`.
- `TestRenderRich_ListItemBlocks` (Review Focus 5): `PageBlockList{Items: [PageListItemBlocks{Blocks: [para(p("one")), para(p("two"))]}]}` → `- one\n\n  two`.
- `TestRenderRich_Post30Shape` (acceptance criterion 11): Heading1, paragraph with a link to `https://t.me/ii_bez_vody/29`, Heading2, paragraph with `TextBold`, list of two text items, Heading4. Assert: output starts with `# `, contains `\n## `, `\n#### `, `[прошлом посте](https://t.me/ii_bez_vody/29)` and `- `.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tg -run TestRenderRich`
Expected: FAIL, `undefined: RenderRich`.

- [ ] **Step 3: Implement `RenderRich` in `internal/tg/richtext.go`**

Use two functions: `renderBlock(b tgapi.PageBlockClass) string` and `renderRichText(t tgapi.RichTextClass) string`. The block and text mapping are the spec tables, "Rich posts: blocks → Markdown". Use `default:` branches that return inner text through the `GetText()`/`GetBlocks()` getters when the type has them, else `""`. Inside `TextFixed`, use only plain inner text, with no markers. Multi-line block prefixes (`> `, list indent) go through one helper, `prefixLines(s, first, rest string) string`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tg -run TestRenderRich -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
go vet ./... && go test ./...
git add internal/tg/richtext.go internal/tg/richtext_test.go
git commit -m "feat(tg): render rich posts as Markdown"
```

### Task 3: Use the renderers in `RenderText`

**Files:**
- Modify: `internal/tg/render.go:79-88` (`RenderText`)
- Test: `internal/tg/render_test.go` (`TestRenderText`)

**Interfaces:**
- Consumes: `RenderEntities` (Task 1), `RenderRich` (Task 2).
- Produces: `RenderText(m *tgapi.Message) string`. The signature does not change.

- [ ] **Step 1: Add failing rows to `TestRenderText`**

```go
{"entities", &tgapi.Message{Message: "hi you", Entities: ents(&tgapi.MessageEntityBold{Offset: 3, Length: 3})}, "hi **you**"},
{"photo caption entities", &tgapi.Message{Message: "cap", Entities: ents(&tgapi.MessageEntityItalic{Length: 3}), Media: &tgapi.MessageMediaPhoto{}}, "[photo] *cap*"},
{"rich", richMsg(&tgapi.PageBlockHeading1{Text: &tgapi.TextPlain{Text: "T"}}, para(p("body"))), "# T\n\nbody"},
{"rich with media", withMedia(richMsg(para(p("a\nb")))), "[photo]\n\na\nb"},
```

`richMsg(blocks...)` builds a `*tgapi.Message` and sets the rich message with `m.SetRichMessage(tgapi.RichMessage{Blocks: blocks})`, so the flag bit is set. All existing rows must still pass unchanged.

- [ ] **Step 2: Run to verify the new rows fail**

Run: `go test ./internal/tg -run TestRenderText`
Expected: FAIL on `entities`, `rich` and `rich with media`.

- [ ] **Step 3: Change `RenderText`**

The body is `RenderRich(rm)` when `rm, ok := m.GetRichMessage(); ok`, else `RenderEntities(m.Message, m.Entities)`. The join is `placeholder + " " + body`. For a rich post whose body has a `\n`, the join is `placeholder + "\n\n" + body`.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/tg -run TestRenderText -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
go vet ./... && go test ./...
git add internal/tg/render.go internal/tg/render_test.go
git commit -m "feat(tg): read_chat text is Markdown"
```

### Task 4: Fetch partial rich posts in `ReadChat`

**Files:**
- Modify: `internal/tg/history.go` (`ReadChat`, before the render loop)
- Test: `internal/tg/history_test.go`

**Interfaces:**
- Consumes: `RenderText` (Task 3), `s.api.MessagesGetRichMessage(ctx, &tgapi.MessagesGetRichMessageRequest{Peer: peer, ID: id}) (tgapi.MessagesMessagesClass, error)`.
- Produces: `func (s *Service) completeRich(ctx context.Context, peer tgapi.InputPeerClass, m *tgapi.Message)`. It replaces the message's rich content in place with the full post. On failure, it appends a `PageBlockParagraph{Text: &tgapi.TextPlain{Text: "[truncated]"}}` block and logs `slog.Warn("get rich message", "id", m.ID, "err", err)`.

- [ ] **Step 1: Write failing tests**

- `TestReadChat_PartialRichFetchesFull`: history returns message 5 with `RichMessage{Part: true, Blocks: [para(p("start"))]}`. Expect `MessagesGetRichMessageRequest` with the same peer and `ID: 5`. Reply with `MessagesMessages{Messages: [msg 5 with Blocks [para(p("start")), para(p("end"))]]}`. Assert `Text == "start\n\nend"`. The mock fails if a second request is made.
- `TestReadChat_PartialRichFetchError`: the same, but `getRichMessage` returns an RPC error. Assert `err == nil` and `Text == "start\n\n[truncated]"`.
- `TestReadChat_PartialRichMissingInResponse`: the response has no message with id 5. Expect the same result as the error case.
- `TestReadChat_CompleteRichNoExtraCall`: `Part: false`. Only the history request is expected.

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/tg -run TestReadChat_.*Rich`
Expected: FAIL. The partial tests show text `start` and an unexpected or missing call.

- [ ] **Step 3: Implement `completeRich` and call it from `ReadChat`**

Call it for each `*tgapi.Message` whose `GetRichMessage()` has `Part == true`, one at a time, before `RenderMessage`. Find the message in the response by `ID == m.ID`. Store users and chats from the response in the peer cache, the same as `getMessage` in `download.go` does.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/tg -v -run TestReadChat`
Expected: PASS, the old `TestReadChat_*` tests included.

- [ ] **Step 5: Commit**

```bash
go vet ./... && go test ./...
git add internal/tg/history.go internal/tg/history_test.go
git commit -m "feat(tg): fetch full rich post when read_chat gets a partial one"
```

### Task 5: Docs and manual check

**Files:**
- Modify: `internal/mcpserver/server.go:88` (`readChatDescription`)
- Modify: `README.md` (`read_chat` section, lines 115–147)

- [ ] **Step 1: Update `readChatDescription`**

Replace "Read plain-text message history" with: "Read message history of one chat, newest first. Message text is Markdown (no escaping); rich posts render with headings, lists and links." Keep the rest of the description.

- [ ] **Step 2: Update README `read_chat`**

- The `text` row: Markdown, see below.
- Add a short subsection "Text format":
  - the shared marker table from the spec
  - no escaping
  - underline, mentions, hashtags and plain URLs stay as text
  - rich posts: `HeadingN` → N `#`, lists, quotes, tables, code
  - media inside rich posts are placeholders and cannot be downloaded
  - a partial rich post costs one extra call; if that call fails, the text ends with `[truncated]`

- [ ] **Step 3: Build and run all checks**

Run: `go vet ./... && go test ./... && go build -o telegram-mcp ./cmd/telegram-mcp`
Expected: no output from vet, all packages `ok`, and a binary is built.

- [ ] **Step 4: Manual checks (acceptance criteria 15–16)**

The owner points the `telegram` MCP server at the worktree binary, or copies the binary to the main checkout path, and restarts the session. Then:
- `read_chat` on `chat_id 3738506469`, `channel`, `limit 3`: posts 28, 29 and 30 have non-empty text. Post 30 starts with `# Агент как сотрудник` (or the heading level its `Heading` block has) and contains `(https://t.me/ii_bez_vody/29)` and `(https://t.me/ii_bez_vody/21)`.
- `read_chat` on a private chat with formatted messages: bold and links show as Markdown, and plain messages are unchanged.

- [ ] **Step 5: Commit**

```bash
git add README.md internal/mcpserver/server.go
git commit -m "docs: read_chat returns Markdown"
```
