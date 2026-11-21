package tg

import (
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf16"

	tgapi "github.com/gotd/td/tg"
)

// marker is the Markdown written before and after a styled run of text.
type marker struct{ open, close string }

type styleKind int

const (
	styleBold styleKind = iota
	styleItalic
	styleStrike
	styleCode
	styleSpoiler
	styleMarked
)

// styleMarker returns the shared inline marker for a style.
func styleMarker(kind styleKind) marker {
	switch kind {
	case styleBold:
		return marker{"**", "**"}
	case styleItalic:
		return marker{"*", "*"}
	case styleStrike:
		return marker{"~~", "~~"}
	case styleCode:
		return marker{"`", "`"}
	case styleSpoiler:
		return marker{"||", "||"}
	case styleMarked:
		return marker{"==", "=="}
	}
	return marker{}
}

// linkMarker returns the marker for [text](url). A URL with ")" or a space
// is written in angle brackets.
func linkMarker(url string) marker {
	if strings.ContainsAny(url, ") ") {
		return marker{"[", "](<" + url + ">)"}
	}
	return marker{"[", "](" + url + ")"}
}

// userURL is the link target for a mention of a user by id.
func userURL(id int64) string {
	return "tg://user?id=" + strconv.FormatInt(id, 10)
}

// codeSpan wraps s in a backtick run one longer than the longest run inside s.
func codeSpan(s string) string {
	ticks := strings.Repeat("`", longestRun(s, false)+1)
	if strings.HasPrefix(s, "`") || strings.HasSuffix(s, "`") {
		s = " " + s + " "
	}
	return ticks + s + ticks
}

// fence writes body as a fenced code block. The fence is longer than any
// backtick run at the start of a line in body.
func fence(lang, body string) string {
	f := strings.Repeat("`", max(3, longestRun(body, true)+1))
	if !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	return f + lang + "\n" + body + f
}

// longestRun returns the longest run of backticks in s. With lineStart, only
// runs at the start of a line (after optional spaces) count.
func longestRun(s string, lineStart bool) int {
	best, run, atStart := 0, 0, true
	for _, r := range s {
		switch {
		case r == '`' && (!lineStart || atStart):
			run++
			best = max(best, run)
			continue
		case r == ' ' && lineStart && atStart && run == 0:
			continue
		}
		run = 0
		atStart = r == '\n'
	}
	return best
}

type spanKind int

// Inline kinds come first; the order breaks ties when spans share a range.
const (
	spanStyle spanKind = iota
	spanLink
	spanCode
	spanPre
	spanQuote
)

type span struct {
	start, end int // UTF-16 offsets, [start, end)
	kind       spanKind
	style      styleKind
	m          marker
	lang       string
}

func (s span) block() bool { return s.kind == spanPre || s.kind == spanQuote }

// RenderEntities renders Telegram message text with its entities as Markdown.
// Offsets in entities are UTF-16. Entity types without a Markdown form are
// written as plain text. Without entities the text is returned as it is.
func RenderEntities(text string, ents []tgapi.MessageEntityClass) string {
	if len(ents) == 0 {
		return text
	}
	r := newEntityRenderer(text)
	var spans []span
	for _, e := range ents {
		s, ok := toSpan(e)
		if !ok {
			continue
		}
		s.start, s.end = r.clamp(e.GetOffset(), e.GetOffset()+e.GetLength())
		if s.start < s.end {
			spans = append(spans, s)
		}
	}
	if len(spans) == 0 {
		return text
	}
	return r.render(0, len(r.u), r.merge(spans))
}

// merge joins style and link spans with the same marker that overlap, touch
// or have only whitespace between them. Telegram often splits one styled run
// into several entities; without this each part gets its own markers, as in
// "**a****b**", which breaks the Markdown.
func (r *entityRenderer) merge(spans []span) []span {
	sort.SliceStable(spans, func(x, y int) bool { return spans[x].start < spans[y].start })
	var out []span
	for _, s := range spans {
		merged := false
		if s.kind == spanStyle || s.kind == spanLink {
			for i := range out {
				o := &out[i]
				if o.kind == s.kind && o.m == s.m && r.joinable(o.end, s.start) {
					o.end = max(o.end, s.end)
					merged = true
					break
				}
			}
		}
		if !merged {
			out = append(out, s)
		}
	}
	return out
}

// joinable reports whether a span ending at end can be joined with one
// starting at start: they overlap, touch or have only whitespace between them.
func (r *entityRenderer) joinable(end, start int) bool {
	for i := end; i < start; i++ {
		if !r.isSpace(i) {
			return false
		}
	}
	return true
}

func toSpan(e tgapi.MessageEntityClass) (span, bool) {
	style := func(k styleKind) (span, bool) {
		return span{kind: spanStyle, style: k, m: styleMarker(k)}, true
	}
	switch e := e.(type) {
	case *tgapi.MessageEntityBold:
		return style(styleBold)
	case *tgapi.MessageEntityItalic:
		return style(styleItalic)
	case *tgapi.MessageEntityStrike:
		return style(styleStrike)
	case *tgapi.MessageEntitySpoiler:
		return style(styleSpoiler)
	case *tgapi.MessageEntityCode:
		return span{kind: spanCode}, true
	case *tgapi.MessageEntityTextURL:
		return span{kind: spanLink, m: linkMarker(e.URL)}, true
	case *tgapi.MessageEntityMentionName:
		return span{kind: spanLink, m: linkMarker(userURL(e.UserID))}, true
	case *tgapi.MessageEntityPre:
		return span{kind: spanPre, lang: e.Language}, true
	case *tgapi.MessageEntityBlockquote:
		return span{kind: spanQuote}, true
	}
	return span{}, false
}

type entityRenderer struct {
	text   string
	u      []uint16
	byteAt []int // byteAt[i] is the byte offset in text of UTF-16 unit i
}

func newEntityRenderer(text string) *entityRenderer {
	r := &entityRenderer{text: text}
	for i, c := range text {
		n := utf16.RuneLen(c)
		if n < 1 {
			n = 1 // invalid UTF-8 or a lone surrogate: one unit
		}
		for k := 0; k < n; k++ {
			r.byteAt = append(r.byteAt, i)
			r.u = append(r.u, 0)
		}
		if n == 2 {
			hi, lo := utf16.EncodeRune(c)
			r.u[len(r.u)-2], r.u[len(r.u)-1] = uint16(hi), uint16(lo)
		} else {
			r.u[len(r.u)-1] = uint16(c)
		}
	}
	r.byteAt = append(r.byteAt, len(text))
	return r
}

// clamp limits [a, b) to the text and moves edges that split a surrogate
// pair outward.
func (r *entityRenderer) clamp(a, b int) (int, int) {
	a = min(max(a, 0), len(r.u))
	b = min(max(b, a), len(r.u))
	if r.splitsPair(a) {
		a--
	}
	if r.splitsPair(b) {
		b++
	}
	return a, b
}

func (r *entityRenderer) splitsPair(i int) bool {
	if i <= 0 || i >= len(r.u) {
		return false
	}
	high, low := r.u[i-1], r.u[i]
	return high >= 0xD800 && high < 0xDC00 && low >= 0xDC00 && low < 0xE000
}

func (r *entityRenderer) slice(a, b int) string { return r.text[r.byteAt[a]:r.byteAt[b]] }

// render writes [lo, hi): block spans as fenced code or quotes, the rest inline.
func (r *entityRenderer) render(lo, hi int, spans []span) string {
	var blocks []int
	for i, s := range spans {
		if s.block() && s.start < hi && s.end > lo {
			blocks = append(blocks, i)
		}
	}
	sort.SliceStable(blocks, func(x, y int) bool {
		a, b := spans[blocks[x]], spans[blocks[y]]
		if a.start != b.start {
			return a.start < b.start
		}
		return a.end > b.end
	})

	var sb strings.Builder
	pos := lo
	for _, bi := range blocks {
		b := spans[bi]
		start, end := max(b.start, lo, pos), min(b.end, hi)
		if start >= end {
			continue // inside the previous block
		}
		sb.WriteString(r.inline(pos, start, spans))
		if sb.Len() > 0 && !strings.HasSuffix(sb.String(), "\n") {
			sb.WriteByte('\n')
		}
		if b.kind == spanPre {
			sb.WriteString(fence(b.lang, r.slice(start, end)))
		} else {
			rest := make([]span, 0, len(spans)-1)
			rest = append(rest, spans[:bi]...)
			rest = append(rest, spans[bi+1:]...)
			sb.WriteString(quote(r.render(start, end, rest)))
		}
		if end < hi && r.u[end] != '\n' && !strings.HasSuffix(sb.String(), "\n") {
			sb.WriteByte('\n')
		}
		pos = end
	}
	sb.WriteString(r.inline(pos, hi, spans))
	return sb.String()
}

// quote prefixes every line of s with "> ".
func quote(s string) string {
	trail := strings.HasSuffix(s, "\n")
	s = strings.TrimSuffix(s, "\n")
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if l == "" {
			lines[i] = ">"
		} else {
			lines[i] = "> " + l
		}
	}
	out := strings.Join(lines, "\n")
	if trail {
		out += "\n"
	}
	return out
}

// inline writes [lo, hi) with the inline spans clipped to it. Overlapping
// spans are split so the markers nest.
func (r *entityRenderer) inline(lo, hi int, all []span) string {
	if lo >= hi {
		return ""
	}
	var spans []span
	for _, s := range all {
		if s.block() {
			continue
		}
		s.start, s.end = max(s.start, lo), min(s.end, hi)
		if s.kind != spanCode {
			for s.start < s.end && r.isSpace(s.start) {
				s.start++
			}
			for s.start < s.end && r.isSpace(s.end-1) {
				s.end--
			}
		}
		if s.start < s.end {
			spans = append(spans, s)
		}
	}
	// Drop spans that lie fully inside a code span: code text has no markers.
	kept := spans[:0]
	for _, s := range spans {
		if s.kind != spanCode && insideCode(s, spans) {
			continue
		}
		kept = append(kept, s)
	}
	spans = kept

	cuts := []int{lo, hi}
	for _, s := range spans {
		cuts = append(cuts, s.start, s.end)
	}
	sort.Ints(cuts)

	var sb strings.Builder
	var codeBuf *strings.Builder
	var stack []int // indices into spans, outermost first
	closeTop := func() {
		top := spans[stack[len(stack)-1]]
		stack = stack[:len(stack)-1]
		if top.kind == spanCode {
			sb.WriteString(codeSpan(codeBuf.String()))
			codeBuf = nil
			return
		}
		sb.WriteString(top.m.close)
	}
	for i := 0; i+1 < len(cuts); i++ {
		a, b := cuts[i], cuts[i+1]
		if a == b {
			continue
		}
		active := activeSpans(spans, a, b)
		common := 0
		for common < len(stack) && common < len(active) && stack[common] == active[common] {
			common++
		}
		for len(stack) > common {
			closeTop()
		}
		for _, si := range active[common:] {
			stack = append(stack, si)
			if spans[si].kind == spanCode {
				codeBuf = &strings.Builder{}
			} else {
				sb.WriteString(spans[si].m.open)
			}
		}
		if codeBuf != nil {
			codeBuf.WriteString(r.slice(a, b))
		} else {
			sb.WriteString(r.slice(a, b))
		}
	}
	for len(stack) > 0 {
		closeTop()
	}
	return sb.String()
}

// activeSpans returns the spans covering [a, b), outermost first. At most one
// code span is kept, and it is always innermost.
func activeSpans(spans []span, a, b int) []int {
	var active []int
	code := -1
	for i, s := range spans {
		if s.start > a || s.end < b {
			continue
		}
		if s.kind == spanCode {
			if code < 0 {
				code = i
			}
			continue
		}
		active = append(active, i)
	}
	sort.SliceStable(active, func(x, y int) bool {
		p, q := spans[active[x]], spans[active[y]]
		if p.start != q.start {
			return p.start < q.start
		}
		if p.end != q.end {
			return p.end > q.end
		}
		if p.kind != q.kind {
			return p.kind < q.kind
		}
		return p.style < q.style
	})
	if code >= 0 {
		active = append(active, code)
	}
	return active
}

func insideCode(s span, spans []span) bool {
	for _, c := range spans {
		if c.kind == spanCode && c.start <= s.start && s.end <= c.end {
			return true
		}
	}
	return false
}

func (r *entityRenderer) isSpace(i int) bool { return unicode.IsSpace(rune(r.u[i])) }
