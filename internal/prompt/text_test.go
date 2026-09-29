package prompt

import (
	"strings"
	"testing"
)

func TestSections(t *testing.T) {
	two := Sections("第一段\n\n第二段")
	if len(two) != 2 || two[0].Text != "第一段" || two[1].Text != "第二段" {
		t.Fatalf("sections = %#v", two)
	}
	merged := Sections("场景：\n\n海滩")
	if len(merged) != 1 {
		t.Fatalf("merged len = %d %#v", len(merged), merged)
	}
	if !strings.Contains(merged[0].Text, "场景：") || !strings.Contains(merged[0].Text, "海滩") {
		t.Fatalf("merged text = %q", merged[0].Text)
	}
}

func TestFindAll(t *testing.T) {
	paper := FindAll("喜欢 Paper-cut 风格", "paper-cut")
	if len(paper) != 1 || "喜欢 Paper-cut 风格"[paper[0].Start:paper[0].End] != "Paper-cut" {
		t.Fatalf("paper spans = %#v", paper)
	}
	beach := FindAll("海滩之后还有海滩", "海滩")
	if len(beach) != 2 {
		t.Fatalf("beach spans = %#v", beach)
	}
	for _, sp := range beach {
		if "海滩之后还有海滩"[sp.Start:sp.End] != "海滩" {
			t.Fatalf("span %d:%d is not on a character boundary", sp.Start, sp.End)
		}
	}
}

func TestExcerptAround(t *testing.T) {
	body := strings.Repeat("甲", 200) + "海滩"
	plain := Excerpt(body)
	if !strings.HasPrefix(plain, "甲") {
		t.Fatalf("leading excerpt = %q", plain)
	}
	start := strings.Index(body, "海滩")
	got, _ := ExcerptAround(body, start)
	if !strings.Contains(got, "海滩") {
		t.Fatalf("window = %q", got)
	}
	if got == body || strings.Contains(got, strings.Repeat("甲", 200)) {
		t.Fatalf("window is the full text: %q", got)
	}
	if len([]rune(strings.Trim(got, "…"))) > excerptLimit {
		t.Fatalf("window longer than the excerpt limit: %q", got)
	}
}
