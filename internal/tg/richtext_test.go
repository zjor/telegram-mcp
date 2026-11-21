package tg

import (
	"strings"
	"testing"

	tgapi "github.com/gotd/td/tg"
)

func p(s string) *tgapi.TextPlain { return &tgapi.TextPlain{Text: s} }

func para(t tgapi.RichTextClass) *tgapi.PageBlockParagraph {
	return &tgapi.PageBlockParagraph{Text: t}
}

func rich(blocks ...tgapi.PageBlockClass) tgapi.RichMessage {
	return tgapi.RichMessage{Blocks: blocks}
}

func TestRenderRich_Golden(t *testing.T) {
	cell := func(s string) tgapi.PageTableCell { return tgapi.PageTableCell{Text: p(s)} }
	rm := rich(
		&tgapi.PageBlockHeading1{Text: p("Title")},
		&tgapi.PageBlockHeading2{Text: p("Sub")},
		&tgapi.PageBlockHeading4{Text: p("Deep")},
		para(&tgapi.TextConcat{Texts: []tgapi.RichTextClass{
			p("See "), &tgapi.TextURL{Text: p("post"), URL: "https://t.me/c/29"}, p("."),
		}}),
		para(&tgapi.TextBold{Text: &tgapi.TextItalic{Text: p("bi")}}),
		&tgapi.PageBlockList{Items: []tgapi.PageListItemClass{
			&tgapi.PageListItemText{Text: p("a")},
			&tgapi.PageListItemText{Text: p("b")},
		}},
		&tgapi.PageBlockOrderedList{Items: []tgapi.PageListOrderedItemClass{
			&tgapi.PageListOrderedItemText{Num: "1", Text: p("x")},
			&tgapi.PageListOrderedItemText{Num: "2", Text: p("y")},
		}},
		&tgapi.PageBlockBlockquote{Text: p("q"), Caption: p("Ann")},
		&tgapi.PageBlockPreformatted{Text: p("a := 1"), Language: "go"},
		&tgapi.PageBlockTable{Rows: []tgapi.PageTableRow{
			{Cells: []tgapi.PageTableCell{cell("h1"), cell("h2")}},
			{Cells: []tgapi.PageTableCell{cell("a|b"), cell("c")}},
		}},
		&tgapi.PageBlockDivider{},
		&tgapi.PageBlockAnchor{Name: "x"},
	)
	want := strings.Join([]string{
		"# Title",
		"## Sub",
		"#### Deep",
		"See [post](https://t.me/c/29).",
		"***bi***",
		"- a\n- b",
		"1. x\n2. y",
		"> q\n> — Ann",
		"```go\na := 1\n```",
		"| h1 | h2 |\n| --- | --- |\n| a\\|b | c |",
		"---",
	}, "\n\n")
	if got := RenderRich(rm); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestRenderRich_RichText(t *testing.T) {
	tests := []struct {
		name string
		in   tgapi.RichTextClass
		want string
	}{
		{"fixed drops inner markers", &tgapi.TextFixed{Text: &tgapi.TextBold{Text: p("x")}}, "`x`"},
		{"strike", &tgapi.TextStrike{Text: p("s")}, "~~s~~"},
		{"spoiler", &tgapi.TextSpoiler{Text: p("s")}, "||s||"},
		{"marked", &tgapi.TextMarked{Text: p("m")}, "==m=="},
		{"email", &tgapi.TextEmail{Text: p("m"), Email: "a@b"}, "[m](mailto:a@b)"},
		{"phone", &tgapi.TextPhone{Text: p("t"), Phone: "+1"}, "[t](tel:+1)"},
		{"mention name", &tgapi.TextMentionName{Text: p("Ann"), UserID: 42}, "[Ann](tg://user?id=42)"},
		{"math", &tgapi.TextMath{Source: "x^2"}, "$x^2$"},
		{"image", &tgapi.TextImage{}, "[image]"},
		{"underline", &tgapi.TextUnderline{Text: p("u")}, "u"},
		{"empty", &tgapi.TextEmpty{}, ""},
		{"edge whitespace outside markers", &tgapi.TextBold{Text: p(" b ")}, " **b** "},
		{"custom emoji alt", &tgapi.TextCustomEmoji{Alt: "🙂"}, "🙂"},
		{"unknown wrapper keeps text", &tgapi.TextHashtag{Text: p("#tag")}, "#tag"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RenderRich(rich(para(tt.in))); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRenderRich_Media(t *testing.T) {
	tests := []struct {
		name string
		in   tgapi.PageBlockClass
		want string
	}{
		{"caption", &tgapi.PageBlockPhoto{Caption: tgapi.PageCaption{Text: p("cap")}}, "[photo] cap"},
		{"credit", &tgapi.PageBlockPhoto{Caption: tgapi.PageCaption{Text: p("cap"), Credit: p("Bob")}}, "[photo] cap — Bob"},
		{"no caption", &tgapi.PageBlockPhoto{}, "[photo]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RenderRich(rich(tt.in)); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRenderRich_EmbedPostKeepsText(t *testing.T) {
	got := RenderRich(rich(&tgapi.PageBlockEmbedPost{
		URL:    "u",
		Blocks: []tgapi.PageBlockClass{para(p("inner"))},
	}))
	if want := "[embed u]\n\n> inner"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRenderRich_Collage(t *testing.T) {
	got := RenderRich(rich(&tgapi.PageBlockCollage{
		Items: []tgapi.PageBlockClass{
			&tgapi.PageBlockPhoto{Caption: tgapi.PageCaption{Text: p("one")}},
		},
		Caption: tgapi.PageCaption{Text: p("all")},
	}))
	if want := "[album] all\n\n[photo] one"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRenderRich_Unknown(t *testing.T) {
	if got := RenderRich(rich(&tgapi.PageBlockUnsupported{})); got != "" {
		t.Errorf("unsupported: got %q, want empty", got)
	}
	if got := RenderRich(rich(&tgapi.PageBlockKicker{Text: p("k")})); got != "k" {
		t.Errorf("kicker: got %q, want %q", got, "k")
	}
	_ = RenderRich(rich(&tgapi.PageBlockButtonRow{
		Buttons: []tgapi.PageButton{{Text: p("Go")}, {}},
	}))
}

func TestRenderRich_Empty(t *testing.T) {
	if got := RenderRich(tgapi.RichMessage{}); got != "" {
		t.Errorf("no blocks: got %q, want empty", got)
	}
	got := RenderRich(rich(&tgapi.PageBlockAnchor{}, &tgapi.PageBlockDivider{}, &tgapi.PageBlockAnchor{}))
	if got != "---" {
		t.Errorf("anchors around divider: got %q, want %q", got, "---")
	}
}

func TestRenderRich_ListItemBlocks(t *testing.T) {
	got := RenderRich(rich(&tgapi.PageBlockList{Items: []tgapi.PageListItemClass{
		&tgapi.PageListItemBlocks{Blocks: []tgapi.PageBlockClass{para(p("one")), para(p("two"))}},
	}}))
	if want := "- one\n\n  two"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRenderRich_Post30Shape(t *testing.T) {
	got := RenderRich(rich(
		&tgapi.PageBlockHeading1{Text: p("Заголовок поста")},
		para(&tgapi.TextConcat{Texts: []tgapi.RichTextClass{
			p("Как я писал в "),
			&tgapi.TextURL{Text: p("прошлом посте"), URL: "https://t.me/ii_bez_vody/29"},
			p(", всё просто."),
		}}),
		&tgapi.PageBlockHeading2{Text: p("Раздел")},
		para(&tgapi.TextConcat{Texts: []tgapi.RichTextClass{
			p("Это "), &tgapi.TextBold{Text: p("важно")}, p("."),
		}}),
		&tgapi.PageBlockList{Items: []tgapi.PageListItemClass{
			&tgapi.PageListItemText{Text: p("первое")},
			&tgapi.PageListItemText{Text: p("второе")},
		}},
		&tgapi.PageBlockHeading4{Text: p("Итог")},
	))
	if !strings.HasPrefix(got, "# ") {
		t.Errorf("does not start with a heading:\n%s", got)
	}
	for _, s := range []string{"\n## ", "\n#### ", "[прошлом посте](https://t.me/ii_bez_vody/29)", "- "} {
		if !strings.Contains(got, s) {
			t.Errorf("missing %q in:\n%s", s, got)
		}
	}
}

func TestRenderRich_NestedAndNumbered(t *testing.T) {
	got := RenderRich(rich(
		&tgapi.PageBlockList{Items: []tgapi.PageListItemClass{
			&tgapi.PageListItemBlocks{Blocks: []tgapi.PageBlockClass{
				para(p("outer")),
				&tgapi.PageBlockList{Items: []tgapi.PageListItemClass{&tgapi.PageListItemText{Text: p("inner")}}},
			}},
		}},
		&tgapi.PageBlockOrderedList{Start: 3, Items: []tgapi.PageListOrderedItemClass{
			&tgapi.PageListOrderedItemText{Text: p("c")},
			&tgapi.PageListOrderedItemText{Text: p("d")},
		}},
	))
	if want := "- outer\n\n  - inner\n\n3. c\n4. d"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRenderRich_EmbedPostAuthorDate(t *testing.T) {
	got := RenderRich(rich(&tgapi.PageBlockEmbedPost{
		URL:     "u",
		Author:  "Ann",
		Date:    86400,
		Caption: tgapi.PageCaption{Text: p("cap")},
		Blocks:  []tgapi.PageBlockClass{para(p("inner"))},
	}))
	if want := "[embed u] Ann, 1970-01-02 cap\n\n> inner"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRenderRich_RelatedArticles(t *testing.T) {
	got := RenderRich(rich(&tgapi.PageBlockRelatedArticles{
		Title: p("More"),
		Articles: []tgapi.PageRelatedArticle{
			{URL: "u", Title: "T", Description: "d", Author: "A"},
			{URL: "v", Title: "Only title"},
			{URL: "w", Description: "no title"},
			{Title: "No URL", Author: "B"},
		},
	}))
	want := "More\n\n" +
		"- [T](u) — d (A)\n" +
		"- [Only title](v)\n" +
		"- [w](w) — no title\n" +
		"- No URL (B)"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
