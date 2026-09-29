package web

import (
	"html"
	"html/template"
	"strings"
	"unicode/utf8"

	"prompt-manager/internal/prompt"
)

func excerptHTML(body string, spans []prompt.Span) template.HTML {
	if len(spans) == 0 {
		return template.HTML(html.EscapeString(prompt.Excerpt(body)))
	}
	snippet, local, prefix, suffix := clip(body, spans)
	return template.HTML(prefix + string(markHTML(snippet, local)) + suffix)
}

func markHTML(body string, spans []prompt.Span) template.HTML {
	spans = mergeSpans(spans)
	var b strings.Builder
	pos := 0
	for _, span := range spans {
		if span.Start < pos || span.End > len(body) || span.End < span.Start {
			continue
		}
		b.WriteString(html.EscapeString(body[pos:span.Start]))
		b.WriteString("<mark>")
		b.WriteString(html.EscapeString(body[span.Start:span.End]))
		b.WriteString("</mark>")
		pos = span.End
	}
	b.WriteString(html.EscapeString(body[pos:]))
	return template.HTML(b.String())
}

func clip(body string, spans []prompt.Span) (string, []prompt.Span, string, string) {
	earliest := spans[0].Start
	for _, span := range spans[1:] {
		if span.Start < earliest {
			earliest = span.Start
		}
	}
	if earliest < 0 {
		earliest = 0
	}
	if earliest > len(body) {
		earliest = len(body)
	}
	runes := []rune(body)
	at := utf8.RuneCountInString(body[:earliest])
	from := at - 20
	if from < 0 {
		from = 0
	}
	to := from + 120
	if to > len(runes) {
		to = len(runes)
	}
	bFrom := runeOffset(body, from)
	bTo := runeOffset(body, to)
	snippet := body[bFrom:bTo]
	var local []prompt.Span
	for _, span := range spans {
		if span.End <= bFrom || span.Start >= bTo {
			continue
		}
		start, end := span.Start, span.End
		if start < bFrom {
			start = bFrom
		}
		if end > bTo {
			end = bTo
		}
		local = append(local, prompt.Span{Start: start - bFrom, End: end - bFrom})
	}
	prefix, suffix := "", ""
	if from > 0 {
		prefix = "…"
	}
	if to < len(runes) {
		suffix = "…"
	}
	return snippet, local, prefix, suffix
}

func runeOffset(body string, runeIndex int) int {
	if runeIndex <= 0 {
		return 0
	}
	seen := 0
	for i := range body {
		if seen == runeIndex {
			return i
		}
		seen++
	}
	return len(body)
}

func mergeSpans(spans []prompt.Span) []prompt.Span {
	if len(spans) == 0 {
		return nil
	}
	ordered := append([]prompt.Span(nil), spans...)
	for i := 1; i < len(ordered); i++ {
		for j := i; j > 0 && (ordered[j].Start < ordered[j-1].Start || (ordered[j].Start == ordered[j-1].Start && ordered[j].End < ordered[j-1].End)); j-- {
			ordered[j], ordered[j-1] = ordered[j-1], ordered[j]
		}
	}
	out := []prompt.Span{ordered[0]}
	for _, span := range ordered[1:] {
		last := &out[len(out)-1]
		if span.Start <= last.End {
			if span.End > last.End {
				last.End = span.End
			}
			continue
		}
		out = append(out, span)
	}
	return out
}
