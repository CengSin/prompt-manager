package prompt

import (
	"errors"
	"net/url"
	"strings"
	"unicode"
)

const excerptLimit = 120

var (
	ErrBodyRequired = errors.New("原文必填")
	ErrInvalidURL   = errors.New("example URL 必须是 http 或 https 链接")
)

type Draft struct {
	Body       string
	BodySearch string
	ExampleURL string
	HasExample bool
}

func Validate(body, exampleURL string) (Draft, error) {
	if strings.TrimSpace(body) == "" {
		return Draft{}, ErrBodyRequired
	}
	storedURL, hasURL, err := normalizeURL(exampleURL)
	if err != nil {
		return Draft{}, err
	}
	return Draft{
		Body:       body,
		BodySearch: SearchText(body),
		ExampleURL: storedURL,
		HasExample: hasURL,
	}, nil
}

func SearchText(s string) string {
	return strings.ToLower(s)
}

func Excerpt(body string) string {
	flat := strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").Replace(body)
	runes := []rune(flat)
	if len(runes) <= excerptLimit {
		return flat
	}
	return string(runes[:excerptLimit]) + "…"
}

func normalizeURL(raw string) (string, bool, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", false, nil
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || !parsed.IsAbs() || parsed.Host == "" || parsed.Opaque != "" {
		return "", false, ErrInvalidURL
	}
	if strings.ContainsFunc(parsed.Host, unicode.IsSpace) {
		return "", false, ErrInvalidURL
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", false, ErrInvalidURL
	}
	return trimmed, true, nil
}
