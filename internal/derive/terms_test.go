package derive

import "testing"

func TestParseTermsKeepsCompositionAndDropsFrameSize(t *testing.T) {
	body := "请用垂直构图，输出 1920×1080。"
	raw := []byte(`{
		"keywords": [{"phrase": "垂直构图"}],
		"temperament": [
			{"kind": "composition", "phrase": "垂直构图", "spellings": ["vertical framing"]},
			{"kind": "style", "phrase": "1920×1080"},
			{"kind": "mood", "phrase": "垂直构图"}
		]
	}`)
	terms := ParseTerms(body, raw)
	var composition, frame int
	for _, term := range terms {
		if term.Kind == "composition" && term.Phrase == "垂直构图" {
			composition++
			if body[term.Start:term.End] != "垂直构图" {
				t.Fatalf("span = %q", body[term.Start:term.End])
			}
			if len(term.Spellings) != 1 || term.Spellings[0] != "vertical framing" {
				t.Fatalf("spellings = %#v", term.Spellings)
			}
		}
		if term.Kind != "keyword" && term.Phrase == "1920×1080" {
			frame++
		}
	}
	if composition != 1 || frame != 0 {
		t.Fatalf("composition=%d frame=%d terms=%#v", composition, frame, terms)
	}
}
