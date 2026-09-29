package derive

import (
	"encoding/json"
	"regexp"
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
	Source string
	Start  int
	End    int
	Model  string
	Values []float32
}

type Result struct {
	Terms   []Term
	Vectors []Vector
}

var deliverySpec = regexp.MustCompile(`(?i)^(?:\d+\s*[x×]\s*\d+|\d+(?:\.\d+)?\s*fps|\d+\s*秒|\d+\s*分(?:\s*\d+\s*秒)?)$`)

var termKinds = map[string]string{
	"style":       "style",
	"风格":          "style",
	"medium":      "medium",
	"媒介":          "medium",
	"composition": "composition",
	"构图":          "composition",
	"lighting":    "lighting",
	"光线":          "lighting",
	"genre":       "genre",
	"体裁":          "genre",
}

type termJSON struct {
	Keywords    []phraseJSON `json:"keywords"`
	Temperament []phraseJSON `json:"temperament"`
}

type phraseJSON struct {
	Phrase    string   `json:"phrase"`
	Kind      string   `json:"kind"`
	Spellings []string `json:"spellings"`
}

func ParseTerms(body string, raw []byte) []Term {
	var parsed termJSON
	if err := json.Unmarshal(unwrapJSON(raw), &parsed); err != nil {
		return nil
	}
	var out []Term
	for _, item := range parsed.Keywords {
		if term, ok := ground(body, "keyword", item.Phrase, nil); ok {
			out = append(out, term)
		}
	}
	for _, item := range parsed.Temperament {
		kind, ok := termKinds[strings.TrimSpace(item.Kind)]
		if !ok || deliverySpec.MatchString(strings.TrimSpace(item.Phrase)) {
			continue
		}
		term, ok := ground(body, kind, item.Phrase, item.Spellings)
		if ok {
			out = append(out, term)
		}
	}
	return out
}

func ground(body, kind, phrase string, spellings []string) (Term, bool) {
	phrase = strings.TrimSpace(phrase)
	if phrase == "" {
		return Term{}, false
	}
	start := strings.Index(body, phrase)
	if start < 0 {
		return Term{}, false
	}
	var extra []string
	seen := map[string]bool{prompt.SearchText(phrase): true}
	for _, spelling := range spellings {
		spelling = strings.TrimSpace(spelling)
		if spelling == "" || seen[prompt.SearchText(spelling)] {
			continue
		}
		seen[prompt.SearchText(spelling)] = true
		extra = append(extra, spelling)
	}
	return Term{
		Kind:      kind,
		Phrase:    phrase,
		Start:     start,
		End:       start + len(phrase),
		Spellings: extra,
	}, true
}

func unwrapJSON(raw []byte) []byte {
	text := strings.TrimSpace(string(raw))
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	return []byte(strings.TrimSpace(text))
}
