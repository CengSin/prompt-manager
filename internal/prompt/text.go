package prompt

import (
	"strings"
	"unicode/utf8"
)

type Span struct {
	Start int
	End   int
}

type Section struct {
	Text  string
	Start int
	End   int
}

func Sections(body string) []Section {
	blocks := splitBlocks(body)
	var out []Section
	for i := 0; i < len(blocks); i++ {
		if headingOnly(blocks[i].Text) && i+1 < len(blocks) {
			next := blocks[i+1]
			out = append(out, Section{
				Text:  body[blocks[i].Start:next.End],
				Start: blocks[i].Start,
				End:   next.End,
			})
			i++
			continue
		}
		out = append(out, blocks[i])
	}
	return out
}

func FindAll(body, needle string) []Span {
	if needle == "" || body == "" {
		return nil
	}
	lowerBody := strings.ToLower(body)
	lowerNeedle := strings.ToLower(needle)
	if len(lowerBody) != len(body) || len(lowerNeedle) != len(needle) {
		return findAllFold(body, needle)
	}
	var spans []Span
	from := 0
	for from < len(lowerBody) {
		rel := strings.Index(lowerBody[from:], lowerNeedle)
		if rel < 0 {
			break
		}
		start := from + rel
		end := start + len(lowerNeedle)
		if utf8.ValidString(body[start:end]) && runeStart(body, start) && runeStart(body, end) {
			spans = append(spans, Span{Start: start, End: end})
			from = end
			continue
		}
		from = start + 1
	}
	return spans
}

func ExcerptAround(body string, start int) (string, bool) {
	if start < 0 || start > len(body) {
		return Excerpt(body), false
	}
	runes := []rune(body)
	at := utf8.RuneCountInString(body[:start])
	from := at - 20
	if from < 0 {
		from = 0
	}
	to := from + excerptLimit
	if to > len(runes) {
		to = len(runes)
	}
	if at >= to && at < len(runes) {
		to = at + 1
		from = to - excerptLimit
		if from < 0 {
			from = 0
		}
	}
	snippet := string(runes[from:to])
	truncated := from > 0 || to < len(runes)
	if from > 0 {
		snippet = "…" + snippet
	}
	if to < len(runes) {
		snippet += "…"
	}
	return snippet, truncated || len([]rune(snippet)) < len(runes)
}

func splitBlocks(body string) []Section {
	var blocks []Section
	start := 0
	i := 0
	for i < len(body) {
		sep, next, ok := blankLineAt(body, i)
		if !ok {
			i++
			continue
		}
		if sep > start && strings.TrimSpace(body[start:sep]) != "" {
			blocks = append(blocks, Section{Text: body[start:sep], Start: start, End: sep})
		}
		start = next
		i = next
	}
	if start < len(body) && strings.TrimSpace(body[start:]) != "" {
		blocks = append(blocks, Section{Text: body[start:], Start: start, End: len(body)})
	}
	return blocks
}

func blankLineAt(body string, i int) (contentEnd, next int, ok bool) {
	if i >= len(body) || body[i] != '\n' {
		return 0, 0, false
	}
	j := i + 1
	for j < len(body) && (body[j] == ' ' || body[j] == '\t' || body[j] == '\r') {
		j++
	}
	if j >= len(body) || body[j] != '\n' {
		return 0, 0, false
	}
	k := j + 1
	for {
		m := k
		for m < len(body) && (body[m] == ' ' || body[m] == '\t' || body[m] == '\r') {
			m++
		}
		if m < len(body) && body[m] == '\n' {
			k = m + 1
			continue
		}
		return i, k, true
	}
}

func headingOnly(text string) bool {
	line := strings.TrimSpace(text)
	if line == "" || strings.Contains(line, "\n") {
		return false
	}
	if strings.HasPrefix(line, "【") && strings.HasSuffix(line, "】") {
		return true
	}
	return strings.HasSuffix(line, "：") || strings.HasSuffix(line, ":")
}

func runeStart(s string, i int) bool {
	return i == 0 || i == len(s) || utf8.RuneStart(s[i])
}

func findAllFold(body, needle string) []Span {
	needleRunes := []rune(strings.ToLower(needle))
	if len(needleRunes) == 0 {
		return nil
	}
	type loc struct {
		lower rune
		start int
		end   int
	}
	var locs []loc
	for start, r := range body {
		lowered := []rune(strings.ToLower(string(r)))
		if len(lowered) != 1 {
			return nil
		}
		locs = append(locs, loc{lower: lowered[0], start: start, end: start + utf8.RuneLen(r)})
	}
	var spans []Span
	for i := 0; i+len(needleRunes) <= len(locs); i++ {
		match := true
		for j := range needleRunes {
			if locs[i+j].lower != needleRunes[j] {
				match = false
				break
			}
		}
		if match {
			spans = append(spans, Span{Start: locs[i].start, End: locs[i+len(needleRunes)-1].end})
			i += len(needleRunes) - 1
		}
	}
	return spans
}
