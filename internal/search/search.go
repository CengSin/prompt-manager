package search

import (
	"math"
	"sort"
	"strings"

	"prompt-manager/internal/prompt"
)

type Term struct {
	Kind      string
	Phrase    string
	Start     int
	End       int
	Spellings []string
}

type Vector struct {
	Start  int
	End    int
	Model  string
	Values []float32
}

type Doc struct {
	ID    string
	Body  string
	Terms []Term
	Vecs  []Vector
}

type Hit struct {
	Index int
	Spans []prompt.Span
	Score float64
}

func Literal(docs []Doc, query string) []Hit {
	pieces := strings.Fields(query)
	if len(pieces) == 0 {
		return nil
	}
	var hits []Hit
	for i, doc := range docs {
		if !matchesAll(doc, pieces) {
			continue
		}
		hits = append(hits, Hit{Index: i, Spans: literalSpans(doc, pieces)})
	}
	return hits
}

func Meaning(docs []Doc, queryVec []float32, model string, min float64, limit int) []Hit {
	if len(queryVec) == 0 || limit <= 0 {
		return nil
	}
	var hits []Hit
	for i, doc := range docs {
		best := 0.0
		var span prompt.Span
		found := false
		for _, vec := range doc.Vecs {
			if vec.Model != model || len(vec.Values) != len(queryVec) {
				continue
			}
			score := cosine(queryVec, vec.Values)
			if !found || score > best {
				best = score
				span = prompt.Span{Start: vec.Start, End: vec.End}
				found = true
			}
		}
		if found && best >= min {
			hits = append(hits, Hit{Index: i, Score: best, Spans: []prompt.Span{span}})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		return hits[i].Score > hits[j].Score
	})
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits
}

func matchesAll(doc Doc, pieces []string) bool {
	for _, piece := range pieces {
		if !pieceMatches(doc, piece) {
			return false
		}
	}
	return true
}

func pieceMatches(doc Doc, piece string) bool {
	needle := prompt.SearchText(piece)
	if strings.Contains(prompt.SearchText(doc.Body), needle) {
		return true
	}
	for _, term := range doc.Terms {
		if strings.Contains(prompt.SearchText(term.Phrase), needle) {
			return true
		}
		for _, spelling := range term.Spellings {
			if strings.Contains(prompt.SearchText(spelling), needle) {
				return true
			}
		}
	}
	return false
}

func literalSpans(doc Doc, pieces []string) []prompt.Span {
	var spans []prompt.Span
	for _, piece := range pieces {
		if found := prompt.FindAll(doc.Body, piece); len(found) > 0 {
			spans = append(spans, found...)
			continue
		}
		needle := prompt.SearchText(piece)
		for _, term := range doc.Terms {
			matched := false
			for _, spelling := range term.Spellings {
				if strings.Contains(prompt.SearchText(spelling), needle) {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
			if term.Start >= 0 && term.End <= len(doc.Body) && term.End > term.Start {
				spans = append(spans, prompt.Span{Start: term.Start, End: term.End})
			}
			spans = append(spans, prompt.FindAll(doc.Body, term.Phrase)...)
		}
	}
	return dedupeSpans(spans)
}

func dedupeSpans(spans []prompt.Span) []prompt.Span {
	if len(spans) == 0 {
		return nil
	}
	sort.Slice(spans, func(i, j int) bool {
		if spans[i].Start == spans[j].Start {
			return spans[i].End < spans[j].End
		}
		return spans[i].Start < spans[j].Start
	})
	out := []prompt.Span{spans[0]}
	for _, sp := range spans[1:] {
		last := &out[len(out)-1]
		if sp.Start <= last.End {
			if sp.End > last.End {
				last.End = sp.End
			}
			continue
		}
		out = append(out, sp)
	}
	return out
}

func cosine(a, b []float32) float64 {
	var dot, na, nb float64
	for i := range a {
		af := float64(a[i])
		bf := float64(b[i])
		dot += af * bf
		na += af * af
		nb += bf * bf
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}
