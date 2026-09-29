package search

import "testing"

func TestLiteralPiecesAndSpelling(t *testing.T) {
	docs := []Doc{
		{ID: "both", Body: "开头是垂直构图，结尾是海滩"},
		{ID: "beach", Body: "只有海滩"},
		{ID: "alias", Body: "喜欢剪纸风格", Terms: []Term{{
			Kind: "style", Phrase: "剪纸风格", Start: len("喜欢"), End: len("喜欢剪纸风格"),
			Spellings: []string{"paper-cut"},
		}}},
	}
	got := Literal(docs, "垂直 海滩")
	if len(got) != 1 || docs[got[0].Index].ID != "both" {
		t.Fatalf("literal = %#v", got)
	}
	alias := Literal(docs, "paper-cut")
	if len(alias) != 1 || docs[alias[0].Index].ID != "alias" {
		t.Fatalf("alias = %#v", alias)
	}
	if len(alias[0].Spans) != 1 || docs[0].Body != docs[0].Body {
		t.Fatal("alias highlight missing")
	}
	span := alias[0].Spans[0]
	if docs[alias[0].Index].Body[span.Start:span.End] != "剪纸风格" {
		t.Fatalf("highlighted %q", docs[alias[0].Index].Body[span.Start:span.End])
	}
}

func TestMeaningThresholdCapAndModel(t *testing.T) {
	docs := []Doc{
		{ID: "high", Vecs: []Vector{{Start: 0, End: 2, Model: "m", Values: []float32{1, 0}}}},
		{ID: "low", Vecs: []Vector{{Start: 0, End: 1, Model: "m", Values: []float32{0.6, 0.8}}}},
		{ID: "side", Vecs: []Vector{{Start: 0, End: 1, Model: "m", Values: []float32{0, 1}}}},
		{ID: "other", Vecs: []Vector{{Start: 0, End: 1, Model: "other", Values: []float32{1, 0}}}},
		{ID: "short", Vecs: []Vector{{Start: 0, End: 1, Model: "m", Values: []float32{1}}}},
	}
	query := []float32{1, 0}
	got := Meaning(docs, query, "m", 0.5, 5)
	if len(got) != 2 || docs[got[0].Index].ID != "high" || docs[got[1].Index].ID != "low" {
		t.Fatalf("meaning = %#v", ids(docs, got))
	}
	if got[0].Score < got[1].Score {
		t.Fatalf("scores out of order: %v %v", got[0].Score, got[1].Score)
	}
	capped := Meaning(docs, query, "m", 0.5, 1)
	if len(capped) != 1 || docs[capped[0].Index].ID != "high" {
		t.Fatalf("capped = %#v", ids(docs, capped))
	}
	if len(Meaning(docs, query, "m", 0.99, 5)) != 1 {
		t.Fatal("minimum did not drop the lower score")
	}
}

func TestLiteralSkipsMeaningUse(t *testing.T) {
	docs := []Doc{{ID: "hit", Body: "海滩", Vecs: []Vector{{Model: "m", Values: []float32{1, 0}, End: 1}}}}
	if len(Literal(docs, "海滩")) == 0 {
		t.Fatal("expected a literal hit")
	}
}

func ids(docs []Doc, hits []Hit) []string {
	out := make([]string, len(hits))
	for i, hit := range hits {
		out[i] = docs[hit.Index].ID
	}
	return out
}
