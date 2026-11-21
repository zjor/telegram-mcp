package tg

import (
	"strconv"
	"strings"
	"time"
	"unicode"

	tgapi "github.com/gotd/td/tg"
)

// RenderRich renders a rich post (Instant View page blocks) as Markdown.
// Blocks are separated by one blank line. Unknown block and text types render
// their inner text, or nothing if they have none.
func RenderRich(rm tgapi.RichMessage) string {
	return renderBlocks(rm.Blocks)
}

// renderBlocks renders blocks joined by one blank line. Empty blocks are
// skipped, and the result has no leading or trailing blank lines.
func renderBlocks(blocks []tgapi.PageBlockClass) string {
	parts := make([]string, 0, len(blocks))
	for _, b := range blocks {
		if s := strings.Trim(renderBlock(b), "\n"); strings.TrimSpace(s) != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, "\n\n")
}

func renderBlock(b tgapi.PageBlockClass) string {
	switch b := b.(type) {
	case *tgapi.PageBlockHeading1:
		return heading(1, b.Text)
	case *tgapi.PageBlockHeading2:
		return heading(2, b.Text)
	case *tgapi.PageBlockHeading3:
		return heading(3, b.Text)
	case *tgapi.PageBlockHeading4:
		return heading(4, b.Text)
	case *tgapi.PageBlockHeading5:
		return heading(5, b.Text)
	case *tgapi.PageBlockHeading6:
		return heading(6, b.Text)
	case *tgapi.PageBlockTitle:
		return heading(1, b.Text)
	case *tgapi.PageBlockSubtitle:
		return heading(2, b.Text)
	case *tgapi.PageBlockHeader:
		return heading(2, b.Text)
	case *tgapi.PageBlockSubheader:
		return heading(3, b.Text)
	case *tgapi.PageBlockKicker:
		return renderRichText(b.Text)
	case *tgapi.PageBlockParagraph:
		return renderRichText(b.Text)
	case *tgapi.PageBlockFooter:
		return renderRichText(b.Text)
	case *tgapi.PageBlockAuthorDate:
		return authorDate(b)
	case *tgapi.PageBlockPreformatted:
		return fence(b.Language, plainText(b.Text))
	case *tgapi.PageBlockDivider:
		return "---"
	case *tgapi.PageBlockAnchor:
		return ""
	case *tgapi.PageBlockList:
		return bulletList(b.Items)
	case *tgapi.PageBlockOrderedList:
		return orderedList(b)
	case *tgapi.PageBlockBlockquote:
		return quoteBlock(renderRichText(b.Text), b.Caption)
	case *tgapi.PageBlockPullquote:
		return quoteBlock(renderRichText(b.Text), b.Caption)
	case *tgapi.PageBlockBlockquoteBlocks:
		return quoteBlock(renderBlocks(b.Blocks), b.Caption)
	case *tgapi.PageBlockDetails:
		return joinBlocks(styled(styleBold, renderRichText(b.Title)), renderBlocks(b.Blocks))
	case *tgapi.PageBlockTable:
		return table(b)
	case *tgapi.PageBlockMath:
		return "$$\n" + b.Source + "\n$$"
	case *tgapi.PageBlockPhoto:
		return media("[photo]", b.Caption)
	case *tgapi.PageBlockVideo:
		return media("[video]", b.Caption)
	case *tgapi.PageBlockAudio:
		return media("[audio]", b.Caption)
	case *tgapi.PageBlockDocument:
		return media("[document]", b.Caption)
	case *tgapi.PageBlockMap:
		return media("[location]", b.Caption)
	case *tgapi.PageBlockEmbed:
		return media(embedPlaceholder(b.URL), b.Caption)
	case *tgapi.PageBlockChannel:
		return "[channel]"
	case *tgapi.PageBlockCollage:
		return joinBlocks(media("[album]", b.Caption), renderBlocks(b.Items))
	case *tgapi.PageBlockSlideshow:
		return joinBlocks(media("[album]", b.Caption), renderBlocks(b.Items))
	case *tgapi.PageBlockEmbedPost:
		head := embedPlaceholder(b.URL)
		if by := authorDateText(b.Author, b.Date); by != "" {
			head += " " + by
		}
		return joinBlocks(media(head, b.Caption), prefixLines(renderBlocks(b.Blocks), "> ", "> "))
	case *tgapi.PageBlockCover:
		return renderBlock(b.Cover)
	case *tgapi.PageBlockRelatedArticles:
		return relatedArticles(b)
	case *tgapi.PageBlockButtonRow:
		var texts []string
		for _, btn := range b.Buttons {
			if s := renderRichText(btn.Text); s != "" {
				texts = append(texts, s)
			}
		}
		return strings.Join(texts, " · ")
	case interface{ GetText() tgapi.RichTextClass }:
		return renderRichText(b.GetText())
	case interface{ GetBlocks() []tgapi.PageBlockClass }:
		return renderBlocks(b.GetBlocks())
	}
	return ""
}

// joinBlocks joins non-empty parts with one blank line.
func joinBlocks(parts ...string) string {
	var kept []string
	for _, s := range parts {
		if s != "" {
			kept = append(kept, s)
		}
	}
	return strings.Join(kept, "\n\n")
}

func heading(level int, t tgapi.RichTextClass) string {
	s := strings.ReplaceAll(renderRichText(t), "\n", " ")
	if strings.TrimSpace(s) == "" {
		return ""
	}
	return strings.Repeat("#", level) + " " + s
}

func authorDate(b *tgapi.PageBlockAuthorDate) string {
	return authorDateText(renderRichText(b.Author), b.PublishedDate)
}

// authorDateText renders "author, YYYY-MM-DD" (UTC), leaving out empty parts.
func authorDateText(author string, date int) string {
	parts := []string{}
	if author != "" {
		parts = append(parts, author)
	}
	if date != 0 {
		parts = append(parts, time.Unix(int64(date), 0).UTC().Format("2006-01-02"))
	}
	return strings.Join(parts, ", ")
}

// caption renders a PageCaption as "text — credit", leaving out empty parts.
func caption(c tgapi.PageCaption) string {
	text, credit := renderRichText(c.Text), renderRichText(c.Credit)
	switch {
	case credit == "":
		return text
	case text == "":
		return "— " + credit
	}
	return text + " — " + credit
}

// media renders a placeholder followed by its caption, if any.
func media(placeholder string, c tgapi.PageCaption) string {
	if s := caption(c); s != "" {
		return placeholder + " " + s
	}
	return placeholder
}

func embedPlaceholder(url string) string {
	if url == "" {
		return "[embed]"
	}
	return "[embed " + url + "]"
}

// quoteBlock prefixes body with "> " and adds "> — caption" if the caption is
// not empty.
func quoteBlock(body string, captionText tgapi.RichTextClass) string {
	lines := []string{}
	if body != "" {
		lines = append(lines, body)
	}
	if c := renderRichText(captionText); c != "" {
		lines = append(lines, "— "+c)
	}
	return prefixLines(strings.Join(lines, "\n"), "> ", "> ")
}

// prefixLines writes first before the first line of s and rest before every
// other line. Empty lines get the prefix without trailing spaces.
func prefixLines(s, first, rest string) string {
	if s == "" {
		return ""
	}
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		pre := rest
		if i == 0 {
			pre = first
		}
		if l == "" {
			lines[i] = strings.TrimRight(pre, " ")
		} else {
			lines[i] = pre + l
		}
	}
	return strings.Join(lines, "\n")
}

// listItem renders one list item: marker before the first line, continuation
// lines indented by the marker width.
func listItem(marker, body string) string {
	if body == "" {
		return strings.TrimRight(marker, " ")
	}
	return prefixLines(body, marker, strings.Repeat(" ", len(marker)))
}

func bulletList(items []tgapi.PageListItemClass) string {
	var out []string
	for _, it := range items {
		var body string
		switch it := it.(type) {
		case *tgapi.PageListItemText:
			body = renderRichText(it.Text)
		case *tgapi.PageListItemBlocks:
			body = renderBlocks(it.Blocks)
		}
		out = append(out, listItem("- ", body))
	}
	return strings.Join(out, "\n")
}

func orderedList(b *tgapi.PageBlockOrderedList) string {
	n := 1
	if b.Start != 0 {
		n = b.Start
	}
	var out []string
	for _, it := range b.Items {
		var num, body string
		switch it := it.(type) {
		case *tgapi.PageListOrderedItemText:
			num, body = it.Num, renderRichText(it.Text)
		case *tgapi.PageListOrderedItemBlocks:
			num, body = it.Num, renderBlocks(it.Blocks)
		}
		if num == "" {
			num = strconv.Itoa(n)
		}
		out = append(out, listItem(num+". ", body))
		n++
	}
	return strings.Join(out, "\n")
}

// table renders a GFM table. The first row is the header. A title, if any,
// is a line before the table.
func table(b *tgapi.PageBlockTable) string {
	cols := 0
	for _, r := range b.Rows {
		cols = max(cols, len(r.Cells))
	}
	var lines []string
	if t := renderRichText(b.Title); t != "" {
		lines = append(lines, t)
		if cols > 0 {
			lines = append(lines, "")
		}
	}
	if cols == 0 {
		return strings.Join(lines, "\n")
	}
	row := func(cells []string) string {
		return "| " + strings.Join(cells, " | ") + " |"
	}
	for i, r := range b.Rows {
		cells := make([]string, cols)
		for j, c := range r.Cells {
			s := renderRichText(c.Text)
			s = strings.ReplaceAll(s, "|", `\|`)
			cells[j] = strings.ReplaceAll(s, "\n", " ")
		}
		lines = append(lines, row(cells))
		if i == 0 {
			sep := make([]string, cols)
			for j := range sep {
				sep[j] = "---"
			}
			lines = append(lines, row(sep))
		}
	}
	return strings.Join(lines, "\n")
}

// relatedArticles renders each article as "- [title](url) — description
// (author)", leaving out empty parts.
func relatedArticles(b *tgapi.PageBlockRelatedArticles) string {
	var lines []string
	if t := renderRichText(b.Title); t != "" {
		lines = append(lines, t, "")
	}
	for _, a := range b.Articles {
		title := a.Title
		if title == "" {
			title = a.URL
		}
		line := title
		if a.URL != "" {
			m := linkMarker(a.URL)
			line = m.open + title + m.close
		}
		if a.Description != "" {
			line += " — " + a.Description
		}
		if a.Author != "" {
			line += " (" + a.Author + ")"
		}
		lines = append(lines, "- "+strings.TrimSpace(line))
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n")
}

// renderRichText renders rich text as inline Markdown.
func renderRichText(t tgapi.RichTextClass) string { return richText(t, false) }

// plainText renders rich text without any Markdown markers.
func plainText(t tgapi.RichTextClass) string { return richText(t, true) }

func richText(t tgapi.RichTextClass, plain bool) string {
	inner := func(t tgapi.RichTextClass) string { return richText(t, plain) }
	style := func(kind styleKind, t tgapi.RichTextClass) string {
		if plain {
			return inner(t)
		}
		return styled(kind, inner(t))
	}
	link := func(t tgapi.RichTextClass, url, fallback string) string {
		s := inner(t)
		if s == "" {
			s = fallback
		}
		if plain {
			return s
		}
		return wrap(linkMarker(url), s)
	}
	switch t := t.(type) {
	case *tgapi.TextEmpty:
		return ""
	case *tgapi.TextPlain:
		return t.Text
	case *tgapi.TextConcat:
		var sb strings.Builder
		for _, part := range t.Texts {
			sb.WriteString(inner(part))
		}
		return sb.String()
	case *tgapi.TextBold:
		return style(styleBold, t.Text)
	case *tgapi.TextItalic:
		return style(styleItalic, t.Text)
	case *tgapi.TextStrike:
		return style(styleStrike, t.Text)
	case *tgapi.TextSpoiler:
		return style(styleSpoiler, t.Text)
	case *tgapi.TextMarked:
		return style(styleMarked, t.Text)
	case *tgapi.TextFixed:
		s := plainText(t.Text)
		if plain || s == "" {
			return s
		}
		return codeSpan(s)
	case *tgapi.TextURL:
		return link(t.Text, t.URL, t.URL)
	case *tgapi.TextEmail:
		return link(t.Text, "mailto:"+t.Email, t.Email)
	case *tgapi.TextPhone:
		return link(t.Text, "tel:"+t.Phone, t.Phone)
	case *tgapi.TextMentionName:
		return link(t.Text, userURL(t.UserID), "")
	case *tgapi.TextMath:
		if plain {
			return t.Source
		}
		return "$" + t.Source + "$"
	case *tgapi.TextImage:
		return "[image]"
	case *tgapi.TextCustomEmoji:
		return t.Alt
	case interface{ GetText() tgapi.RichTextClass }:
		return inner(t.GetText())
	}
	return ""
}

// styled wraps s in the marker for kind.
func styled(kind styleKind, s string) string { return wrap(styleMarker(kind), s) }

// wrap puts m tight around each line of s, so no marker spans a newline.
// Leading and trailing whitespace of a line goes outside the markers.
// Whitespace-only lines are returned as they are.
func wrap(m marker, s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = wrapLine(m, l)
	}
	return strings.Join(lines, "\n")
}

func wrapLine(m marker, s string) string {
	core := strings.TrimFunc(s, unicode.IsSpace)
	if core == "" {
		return s
	}
	lead := s[:len(s)-len(strings.TrimLeftFunc(s, unicode.IsSpace))]
	trail := s[len(strings.TrimRightFunc(s, unicode.IsSpace)):]
	return lead + m.open + core + m.close + trail
}
