package tg

import (
	"regexp"
	"strings"
	"testing"

	tgapi "github.com/gotd/td/tg"
)

func ents(e ...tgapi.MessageEntityClass) []tgapi.MessageEntityClass { return e }

type entitiesCase struct {
	name string
	text string
	ents []tgapi.MessageEntityClass
	want string
}

var entitiesCases = []entitiesCase{
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
	// Extra cases beyond the brief.
	{"code partly bold", "abcdef", ents(&tgapi.MessageEntityBold{Length: 5}, &tgapi.MessageEntityCode{Offset: 2, Length: 2}), "**ab`cd`e**f"},
	{"bold in quote", "x\nq1 q2\ny", ents(&tgapi.MessageEntityBlockquote{Offset: 2, Length: 5}, &tgapi.MessageEntityBold{Offset: 0, Length: 4}), "**x**\n> **q1** q2\n\ny"},
	{"pre in quote", "q\nc", ents(&tgapi.MessageEntityBlockquote{Length: 3}, &tgapi.MessageEntityPre{Offset: 2, Length: 1}), "> q\n> ```\n> c\n> ```"},
	{"pre with fence inside", "```\nx", ents(&tgapi.MessageEntityPre{Length: 5}), "````\n```\nx\n````"},
	// Final review fixes.
	{"merge touching bold", "ab", ents(&tgapi.MessageEntityBold{Length: 1}, &tgapi.MessageEntityBold{Offset: 1, Length: 1}), "**ab**"},
	{"merge bold over space", "a b", ents(&tgapi.MessageEntityBold{Length: 2}, &tgapi.MessageEntityBold{Offset: 2, Length: 1}), "**a b**"},
	{"merge bold around link", "a b, c", ents(
		&tgapi.MessageEntityBold{Length: 1},
		&tgapi.MessageEntityBold{Offset: 2, Length: 1}, &tgapi.MessageEntityTextURL{Offset: 2, Length: 1, URL: "u"},
		&tgapi.MessageEntityBold{Offset: 3, Length: 3},
	), "**a [b](u), c**"},
	{"merge nested same style", "abc", ents(&tgapi.MessageEntityBold{Length: 3}, &tgapi.MessageEntityBold{Offset: 1, Length: 1}), "**abc**"},
	{"quote then text", "quoted\nnext", ents(&tgapi.MessageEntityBlockquote{Length: 6}), "> quoted\n\nnext"},
	{"quote mid line", "x quoted y", ents(&tgapi.MessageEntityBlockquote{Offset: 2, Length: 6}), "x \n> quoted\n\n y"},
	{"quote with newline then text", "q\n\nnext", ents(&tgapi.MessageEntityBlockquote{Length: 2}), "> q\n\nnext"},
	{"bold split at blank line", "a\n\nb", ents(&tgapi.MessageEntityBold{Length: 4}), "**a**\n\n**b**"},
	{"link split at newline", "a\nb", ents(&tgapi.MessageEntityTextURL{Length: 3, URL: "u"}), "[a](u)\n[b](u)"},
	{"no merge of different links", "ab", ents(&tgapi.MessageEntityTextURL{Length: 1, URL: "u"}, &tgapi.MessageEntityTextURL{Offset: 1, Length: 1, URL: "v"}), "[a](u)[b](v)"},
}

func TestRenderEntities(t *testing.T) {
	for _, tc := range entitiesCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := RenderEntities(tc.text, tc.ents); got != tc.want {
				t.Errorf("RenderEntities(%q) = %q, want %q", tc.text, got, tc.want)
			}
		})
	}
}

func TestRenderEntities_SplitSurrogate(t *testing.T) {
	got := RenderEntities("😀x", ents(&tgapi.MessageEntityBold{Offset: 1, Length: 2}))
	if n := strings.Count(got, "😀"); n != 1 {
		t.Errorf("got %q: emoji count %d, want 1", got, n)
	}
	if s := stripMarkers(got); s != "😀x" {
		t.Errorf("stripMarkers(%q) = %q, want %q", got, s, "😀x")
	}
}

func TestRenderEntities_NoLostText(t *testing.T) {
	for _, tc := range entitiesCases {
		t.Run(tc.name, func(t *testing.T) {
			got := RenderEntities(tc.text, tc.ents)
			if !isSubsequence([]rune(stripMarkers(tc.text)), []rune(stripMarkers(got))) {
				t.Errorf("text lost: input %q, output %q", tc.text, got)
			}
		})
	}
}

func TestCodeSpan(t *testing.T) {
	for in, want := range map[string]string{
		"ls":    "`ls`",
		"a``b":  "```a``b```",
		"`x":    "`` `x ``",
		"x`":    "`` x` ``",
		"plain": "`plain`",
	} {
		if got := codeSpan(in); got != want {
			t.Errorf("codeSpan(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLinkMarkerAndUserURL(t *testing.T) {
	if m := linkMarker("https://x.io"); m.open != "[" || m.close != "](https://x.io)" {
		t.Errorf("linkMarker plain = %+v", m)
	}
	if m := linkMarker("a b"); m.close != "](<a b>)" {
		t.Errorf("linkMarker space = %+v", m)
	}
	if u := userURL(7); u != "tg://user?id=7" {
		t.Errorf("userURL = %q", u)
	}
	if m := styleMarker(styleMarked); m.open != "==" || m.close != "==" {
		t.Errorf("styleMarker(marked) = %+v", m)
	}
}

var (
	fenceLineRe = regexp.MustCompile("(?m)^`{3,}[^\n]*\n?")
	quoteRe     = regexp.MustCompile("(?m)^> ?")
	linkTailRe  = regexp.MustCompile(`\]\(<[^>]*>\)|\]\([^)]*\)`)
)

// stripMarkers removes Markdown markers produced by RenderEntities.
func stripMarkers(s string) string {
	s = fenceLineRe.ReplaceAllString(s, "")
	s = quoteRe.ReplaceAllString(s, "")
	s = linkTailRe.ReplaceAllString(s, "")
	for _, m := range []string{"**", "*", "~~", "`", "||", "["} {
		s = strings.ReplaceAll(s, m, "")
	}
	return s
}

func isSubsequence(sub, s []rune) bool {
	i := 0
	for _, r := range s {
		if i < len(sub) && sub[i] == r {
			i++
		}
	}
	return i == len(sub)
}
