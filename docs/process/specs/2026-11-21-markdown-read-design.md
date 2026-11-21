# Markdown text in `read_chat` — Design

Date: 2026-11-21
Status: approved

## Intent

Agents (Claude and others) read Telegram history through `read_chat`. Today
the `text` field is plain text, and two things go wrong:

1. **Rich posts are empty.** Telegram's new rich posts keep their content in
   `Message.RichMessage` (a list of Instant View page blocks). `Message.Message`
   is `""` and `Message.Media` is `nil`. `read_chat` returns `"text": ""`.
   Example: channel `ii_bez_vody`, posts 28–30.
2. **Formatting is lost in ordinary messages.** `RenderText`
   (`internal/tg/render.go:79`) ignores `Message.Entities`. Bold, code,
   quotes and link targets disappear. A text link shows its words but not
   its URL.

The reader is an agent. It needs the structure (headings, lists, quotes,
code) and the link targets. Markdown carries both and agents read it well.
`send_message` already takes Markdown, so the read side and the write side
then use the same language.

## Goals

- `read_chat` returns the `text` of every message as Markdown.
- Rich posts render in full: headings, paragraphs, lists, quotes, code,
  tables and links.
- Ordinary messages render their entities as Markdown.
- Media placeholders stay as they are today (`[photo] caption`).

## Non-goals

- Escaping Markdown characters in the source text (decided, see
  "Escaping").
- Downloading media that sits inside a rich post (photo or video blocks).
  These render as placeholders only. `download_media` is not changed.
- Sending rich posts.
- Markdown in any other tool output.
- A switch for plain text output.

## Anti-goals

- **Lost text.** Every visible character of the source text must appear in
  the output, in order. Formatting may be dropped; text may not.
- **Broken Markdown that hides content.** An unclosed code fence or an
  unclosed marker that swallows the rest of the message is a failure, even
  if each piece is correct alone.
- **A panic or an error on unknown types.** Telegram adds block and entity
  types often. An unknown type renders its inner text, or a placeholder if
  it has none. `read_chat` never fails because of rendering.
- **An extra call per message.** Rendering is local. The only network call
  this feature adds is for partial rich posts (see "Partial rich posts").

## Constraints

- gotd v0.163.0 (`go.mod`). Types used: `tg.Message.Entities`,
  `tg.Message.RichMessage` (a value `tg.RichMessage` with `Rtl`, `Part`,
  `Blocks`, `Photos`, `Documents`; present when `m.GetRichMessage()`
  returns `ok`, flag `Flags2` bit 13), `tg.PageBlockClass` (40 types), `tg.RichTextClass` (32 types),
  `tg.MessageEntityClass` (25 types).
- gotd converts only Markdown → entities and Markdown → rich blocks (for
  sending). It has no converter in the other direction. We write both.
- Entity `Offset` and `Length` count UTF-16 code units, not bytes or runes.
- Posts 28–30 of `ii_bez_vody` use `Heading1`, `Heading2`, `Heading4`,
  `Paragraph`, `List`, `OrderedList` (items `PageListItemText`) and
  `Blockquote`. gotd's own Markdown → rich converter also emits
  `HeadingN` for `#`…`######`. `Title`/`Header`/`Subheader` are the
  older Instant View blocks.
- `read_chat` is the only caller of `RenderText` (`internal/tg/history.go:56`).

## Approaches

1. **Two renderers with one shared marker table (chosen).** `entities.go`
   renders entities with a sweep over boundary points. `richtext.go` renders
   rich blocks with a recursive `switch`. Both take their Markdown markers
   from one table, so bold is `**` in both. Each renderer follows the shape
   of its own data.
2. **Entities → rich text tree → one renderer.** One renderer, but the
   entity-nesting work is still needed to build the tree. `pre` and
   `blockquote` are line entities in messages but blocks in rich posts, so
   the mapping is not 1:1. Rejected: harder converter for no gain.
3. **Rich posts only, entities later.** Rejected by the owner: ordinary
   messages should be Markdown too.

## Output dialect

The target is the "Rich Markdown" dialect that gotd documents in
`telegram/message/rich/spec.md`: CommonMark + GFM tables and strikethrough,
plus `==marked==`, `||spoiler||`, `$math$` and `tg://` links. This is what
Telegram itself parses for rich posts.

### Shared inline markers

| Style | Markdown |
| --- | --- |
| bold | `**text**` |
| italic | `*text*` |
| strike | `~~text~~` |
| inline code | `` `text` `` (longer backtick run if the text has a backtick) |
| spoiler | `\|\|text\|\|` |
| marked (rich only) | `==text==` |
| link | `[text](url)` |
| user mention by id | `[text](tg://user?id=N)` |
| underline | no marker; text only (CommonMark has no underline; same rule as `send_message`) |

Markers are placed tight around the text. If the styled text has leading or
trailing whitespace, that whitespace goes outside the markers
(`**bold** ` not `**bold **`), because CommonMark does not close a marker
after a space.

### Escaping

No escaping. Characters such as `*`, `_`, `[` in the source text are output
as they are (`file_name_v2` stays `file_name_v2`). The readers are agents,
not strict parsers. Code spans and code blocks keep their content as it is
anyway.

## Ordinary messages: entities → Markdown

`RenderEntities(text string, ents []tg.MessageEntityClass) string`.

Mapping:

| Entity | Output |
| --- | --- |
| `Bold`, `Italic`, `Strike`, `Spoiler`, `Code` | shared markers |
| `TextUrl` | `[text](url)` |
| `MentionName` | `[text](tg://user?id=N)` |
| `Pre` | fenced block: ```` ```lang ```` on its own line, the text, ```` ``` ```` on its own line |
| `Blockquote` | `> ` before every line of the quoted range (collapsed quotes too) |
| `Underline` | text only |
| `Mention`, `Hashtag`, `Cashtag`, `BotCommand`, `Url`, `Email`, `Phone`, `BankCard` | text only (the text already is the value) |
| `CustomEmoji`, `FormattedDate`, `Diff*`, `TonAddress`, `Unknown`, any new type | text only |

Rules:

- Offsets are UTF-16. Convert text to UTF-16 once, map entity ranges, then
  write the output from the original runes. A range that is out of bounds
  is clamped; an empty range is ignored.
- Entities can overlap without nesting (bold `[0,10)`, link `[5,15)`). The
  renderer splits them so markers nest correctly: close the inner markers
  at the boundary and reopen them after.
- No inline marker is emitted inside a `Code` or `Pre` range. Their text is
  copied exactly.
- `Pre` and `Blockquote` start on a new line. If the range does not start
  at the beginning of a line, a `\n` is added before it; same at the end.
- A link whose URL contains `)` or a space is written as `[text](<url>)`.
- No entities: the output equals the input exactly.

## Rich posts: blocks → Markdown

`RenderRich(rm tg.RichMessage) string`. Blocks are separated by one blank
line. Leading and trailing blank lines are trimmed.

Block mapping:

| Block | Output |
| --- | --- |
| `Heading1` … `Heading6` | `#` … `######` + text (N `#` for level N) |
| `Title` | `# text` |
| `Subtitle`, `Header` | `## text` |
| `Subheader` | `### text` |
| `Kicker`, `Paragraph`, `Footer` | text |
| `AuthorDate` | `author, date` (date as `YYYY-MM-DD` UTC) |
| `Preformatted` | fenced block with language |
| `Divider` | `---` |
| `Anchor` | nothing |
| `List` | `- item` per item |
| `OrderedList` | `1. item`, `2. item` …, using the item's own number when set |
| `Blockquote`, `Pullquote` | `> text`, then `> — caption` if caption is not empty |
| `BlockquoteBlocks` | the inner blocks, each line prefixed with `> ` |
| `Details` | `**title**`, then the inner blocks |
| `Table` | GFM table; first row is the header; `|` in cells written as `\|`; an empty title is omitted, a title is a line before the table |
| `Math` | `$$` block |
| `Photo`, `Video`, `Audio`, `Document`, `Map`, `Embed`, `Channel` | placeholder (`[photo]`, `[video]`, `[audio]`, `[document]`, `[location]`, `[embed url]`, `[channel]`), then the caption if not empty |
| `Collage`, `Slideshow` | `[album]`, then the caption; then each item rendered as a block (so item captions are kept) |
| `EmbedPost` | `[embed url]`, then its inner `Blocks`, each line prefixed with `> ` |
| `Cover` | the inner block |
| `RelatedArticles` | `- [title](url)` per article |
| `ButtonRow`, `Thinking`, `Unsupported`, any new type | inner text if the type has any, else nothing |

A caption (`PageCaption`) renders as its `Text`, then ` — ` and its
`Credit` if the credit is not empty.

The "no lost text" anti-goal wins over this table: a block that holds
text or inner blocks always renders them after its placeholder.

List items that contain blocks (`PageListItemBlocks`) render the blocks and
indent continuation lines by the list marker width. Nested lists indent by
two spaces per level.

Rich text mapping (`RichTextClass`):

| Text | Output |
| --- | --- |
| `Plain` | text |
| `Concat` | parts joined |
| `Bold`, `Italic`, `Strike`, `Fixed`, `Spoiler`, `Marked` | shared markers |
| `Url`, `Email` (`mailto:`), `Phone` (`tel:`) | link |
| `MentionName` (the only rich text type with a user id) | `[text](tg://user?id=N)` |
| `Math` | `$text$` |
| `Image` | `[image]` |
| `Empty` | nothing |
| everything else (`Underline`, `Subscript`, `Superscript`, `Anchor`, `CustomEmoji`, `Hashtag`, `Cashtag`, `BotCommand`, `Auto*`, `BankCard`, `Date`, `Diff`, `Button`, `TonAddress`, new types) | inner text |

A `Fixed` span inside a link, or a link inside bold, nests like any other
style. Text inside `Fixed` gets no inner markers.

## How `RenderText` changes

```
body := RenderRich(rm)                       if rm, ok := m.GetRichMessage(); ok
        RenderEntities(m.Message, m.Entities) otherwise
```

Then the media placeholder rule stays: `placeholder + " " + body`, or one of
them alone if the other is empty. A rich post with a placeholder and a
multi-line body is written as `[photo]\n\nbody` instead of
`[photo] body`, so the first heading stays on its own line.

### Partial rich posts

When `RichMessage.Part` is true, Telegram sent only the start of the post.
`ReadChat` then calls `messages.getRichMessage(peer, id)` once for that
message and renders the full post from the response. On error, it renders
the partial blocks, appends `\n\n[truncated]`, and logs a warning. It does
not fail the call. The calls are sequential, at most one per partial post
on the page.

`RenderText` stays a pure function. The fetch happens in `ReadChat`
before rendering.

## Docs

- `read_chat` tool description and README: `text` is Markdown (the dialect
  above, no escaping). Add a short table of what entities and blocks become.
- README, `read_chat` section: a rich post renders as Markdown with
  headings; media inside a rich post are placeholders and cannot be
  downloaded.

## Acceptance criteria

Unit tests (in `internal/tg`, table-driven, no network):

1. A message with no entities and no rich content returns its text
   unchanged, byte for byte, including `*`, `_` and `[`.
2. Bold, italic, strike, spoiler and inline code each render with their
   marker from the shared table.
3. A text link renders as `[text](url)`. A `MentionName` renders as
   `[text](tg://user?id=N)`.
4. Offsets with emoji and other non-BMP characters before and inside the
   range render at the right place (UTF-16 offsets).
5. Overlapping entities (bold `[0,10)`, link `[5,15)`) produce correctly
   nested Markdown, and every character of the text appears once, in order.
6. No marker appears inside a `Code` or `Pre` range. `Pre` with language
   `go` renders as a fenced block with ```` ```go ````.
7. A `Blockquote` over two lines renders with `> ` on both lines.
8. Whitespace at the edge of a styled range ends up outside the markers.
9. An unknown entity type and an out-of-range entity do not panic; the text
   is unchanged.
10. A rich post with `Heading1`, `Heading2`, `Heading4`, a paragraph with a link, a bulleted
    list, an ordered list, a quote, a code block and a table renders to the
    expected Markdown (golden string in the test).
11. A rich post built like post 30 of `ii_bez_vody` (`HeadingN`
    blocks, paragraphs with links, a list) renders each `HeadingN` with
    N `#` and links as `[text](url)`, and is not empty.
12. A photo block with a caption renders as `[photo] caption`. A caption
    with a credit renders as `caption — credit`. An `EmbedPost` keeps its
    inner text. An unknown
    block type renders its text or nothing, and does not panic.
13. A message with both media and entities renders as
    `[photo] <markdown caption>`, as today plus formatting.
14. `ReadChat` with a partial rich post calls `messages.getRichMessage` once
    and renders the full post. When that call fails, the result has the
    partial text plus `[truncated]` and the call succeeds.

Manual check:

15. `read_chat` on channel `ii_bez_vody` (id `3738506469`) returns posts
    28, 29 and 30 with non-empty Markdown text. Post 30 starts with its
    title and has the links to posts 29 and 21.
16. `read_chat` on a private chat with formatted messages shows bold and
    links as Markdown and leaves unformatted messages unchanged.

Checks: `go test ./...` and `go vet ./...` pass.

## Left to the builder

- File layout inside `internal/tg` (`entities.go`, `richtext.go` and tests
  are the suggestion).
- The exact sweep algorithm for overlapping entities.
- Placeholder wording for rare block types not listed above.
