package prompt

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateBody(t *testing.T) {
	tests := []struct {
		name string
		body string
		ok   bool
	}{
		{name: "empty", body: "", ok: false},
		{name: "spaces", body: " \n\t  ", ok: false},
		{name: "keeps surrounding whitespace and line breaks", body: "  line\n\tnext  ", ok: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Validate(tt.body, "")
			if tt.ok {
				if err != nil {
					t.Fatalf("Validate: %v", err)
				}
				if got.Body != tt.body {
					t.Fatalf("body = %q, want %q", got.Body, tt.body)
				}
				if got.HasExample {
					t.Fatal("blank URL was stored")
				}
				return
			}
			if !errors.Is(err, ErrBodyRequired) {
				t.Fatalf("err = %v, want ErrBodyRequired", err)
			}
		})
	}
}

func TestValidateExampleURL(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    string
		wantHas bool
		ok      bool
	}{
		{name: "x status", raw: "https://x.com/i/status/1", want: "https://x.com/i/status/1", wantHas: true, ok: true},
		{name: "trims space", raw: "  https://example.com/work  ", want: "https://example.com/work", wantHas: true, ok: true},
		{name: "blank", raw: "  \t", wantHas: false, ok: true},
		{name: "javascript", raw: "javascript:alert(1)", ok: false},
		{name: "relative", raw: "/prompts/1", ok: false},
		{name: "missing host", raw: "http:///nohost", ok: false},
		{name: "empty", raw: "", wantHas: false, ok: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Validate("kept", tt.raw)
			if tt.ok {
				if err != nil {
					t.Fatalf("Validate: %v", err)
				}
				if got.HasExample != tt.wantHas || got.ExampleURL != tt.want {
					t.Fatalf("url = %q has=%v, want %q has=%v", got.ExampleURL, got.HasExample, tt.want, tt.wantHas)
				}
				return
			}
			if !errors.Is(err, ErrInvalidURL) {
				t.Fatalf("err = %v, want ErrInvalidURL", err)
			}
		})
	}
}

func TestSearchTextAndExcerpt(t *testing.T) {
	if got := SearchText("剪纸"); got != "剪纸" {
		t.Fatalf("SearchText(剪纸) = %q", got)
	}
	if got := SearchText("Paper-cut"); got != "paper-cut" {
		t.Fatalf("SearchText(Paper-cut) = %q", got)
	}

	body := "第一行\n" + strings.Repeat("字", 200)
	original := body
	excerpt := Excerpt(body)
	if body != original {
		t.Fatal("Excerpt changed the original text")
	}
	if len([]rune(excerpt)) >= len([]rune(body)) {
		t.Fatalf("excerpt is not shorter than the body: %d vs %d", len([]rune(excerpt)), len([]rune(body)))
	}
	if !strings.HasPrefix(excerpt, "第一行 ") {
		t.Fatalf("excerpt = %q, want the line break rendered as a space", excerpt)
	}
	if !strings.HasSuffix(excerpt, "…") {
		t.Fatalf("excerpt = %q, want an ellipsis", excerpt)
	}
}
