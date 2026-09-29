package derive

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"prompt-manager/internal/prompt"
)

const (
	defaultBase         = "https://openrouter.ai/api/v1"
	defaultSimilarity   = 0.35
	defaultMeaningLimit = 5
	ConfigPath          = "./data/config.json"
)

var ErrNotConfigured = errors.New("model is not configured")

type Config struct {
	TermBaseURL   string
	TermKey       string
	TermModel     string
	EmbedBaseURL  string
	EmbedAPIKey   string
	EmbedModel    string
	SimilarityMin float64
	MeaningLimit  int
	client        *http.Client
}

type endpointFile struct {
	BaseURL string `json:"baseUrl"`
	APIKey  string `json:"apiKey"`
	Model   string `json:"model"`
}

type fileDocument struct {
	Term          endpointFile `json:"term"`
	Embed         endpointFile `json:"embed"`
	SimilarityMin *float64     `json:"similarityMin"`
	MeaningLimit  *int         `json:"meaningLimit"`
}

func LoadFile(path string) Config {
	cfg := Config{
		TermBaseURL:   defaultBase,
		EmbedBaseURL:  defaultBase,
		SimilarityMin: defaultSimilarity,
		MeaningLimit:  defaultMeaningLimit,
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return cfg
	}
	var doc fileDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		return cfg
	}
	cfg.TermBaseURL = baseOrDefault(doc.Term.BaseURL)
	cfg.TermKey = strings.TrimSpace(doc.Term.APIKey)
	cfg.TermModel = strings.TrimSpace(doc.Term.Model)
	cfg.EmbedBaseURL = baseOrDefault(doc.Embed.BaseURL)
	cfg.EmbedAPIKey = strings.TrimSpace(doc.Embed.APIKey)
	cfg.EmbedModel = strings.TrimSpace(doc.Embed.Model)
	if doc.SimilarityMin != nil {
		cfg.SimilarityMin = *doc.SimilarityMin
	}
	if doc.MeaningLimit != nil && *doc.MeaningLimit > 0 {
		cfg.MeaningLimit = *doc.MeaningLimit
	}
	return cfg
}

func baseOrDefault(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultBase
	}
	return strings.TrimRight(raw, "/")
}

func (c Config) TermsReady() bool {
	return c.TermBaseURL != "" && c.TermKey != "" && c.TermModel != ""
}

func (c Config) EmbedReady() bool {
	return c.EmbedBaseURL != "" && c.EmbedAPIKey != "" && c.EmbedModel != ""
}

func (c Config) Ready() bool {
	return c.TermsReady() && c.EmbedReady()
}

func (c Config) Redact(reason string) string {
	for _, secret := range []string{c.TermKey, c.EmbedAPIKey} {
		if secret == "" {
			continue
		}
		reason = strings.ReplaceAll(reason, secret, "")
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return "处理失败"
	}
	return reason
}

func (c Config) httpClient() *http.Client {
	if c.client != nil {
		return c.client
	}
	return &http.Client{Timeout: 60 * time.Second}
}

func (c Config) Derive(ctx context.Context, body string) (Result, error) {
	if !c.Ready() {
		return Result{}, ErrNotConfigured
	}
	terms, err := c.Extract(ctx, body)
	if err != nil {
		return Result{}, err
	}
	sections := prompt.Sections(body)
	inputs := make([]string, 0, len(sections)+len(terms))
	for _, section := range sections {
		inputs = append(inputs, section.Text)
	}
	for _, term := range terms {
		inputs = append(inputs, term.Phrase)
	}
	vectors, err := c.Embed(ctx, inputs)
	if err != nil {
		return Result{}, err
	}
	if len(vectors) != len(inputs) {
		return Result{}, fmt.Errorf("embedding count %d, want %d", len(vectors), len(inputs))
	}
	out := Result{Terms: terms}
	for i, section := range sections {
		out.Vectors = append(out.Vectors, Vector{
			Source: "section",
			Start:  section.Start,
			End:    section.End,
			Model:  c.EmbedModel,
			Values: vectors[i],
		})
	}
	for i, term := range terms {
		out.Vectors = append(out.Vectors, Vector{
			Source: "term",
			Start:  term.Start,
			End:    term.End,
			Model:  c.EmbedModel,
			Values: vectors[len(sections)+i],
		})
	}
	return out, nil
}

func (c Config) EmbedQuery(ctx context.Context, text string) ([]float32, string, error) {
	if !c.EmbedReady() {
		return nil, "", ErrNotConfigured
	}
	vectors, err := c.Embed(ctx, []string{text})
	if err != nil {
		return nil, "", err
	}
	if len(vectors) != 1 {
		return nil, "", fmt.Errorf("embedding count %d", len(vectors))
	}
	return vectors[0], c.EmbedModel, nil
}

func (c Config) Extract(ctx context.Context, body string) ([]Term, error) {
	if !c.TermsReady() {
		return nil, ErrNotConfigured
	}
	payload, err := json.Marshal(map[string]any{
		"model": c.TermModel,
		"messages": []map[string]string{
			{"role": "user", "content": extractPrompt(body)},
		},
	})
	if err != nil {
		return nil, err
	}
	endpoint := strings.TrimRight(c.TermBaseURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.TermKey)
	res, err := c.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	text, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, statusError("term model", res.StatusCode, text)
	}
	parsed, err := completionText(text)
	if err != nil {
		return nil, err
	}
	return ParseTerms(body, parsed), nil
}

func (c Config) Embed(ctx context.Context, inputs []string) ([][]float32, error) {
	if !c.EmbedReady() {
		return nil, ErrNotConfigured
	}
	if len(inputs) == 0 {
		return nil, nil
	}
	payload, err := json.Marshal(map[string]any{
		"model": c.EmbedModel,
		"input": inputs,
	})
	if err != nil {
		return nil, err
	}
	endpoint := strings.TrimRight(c.EmbedBaseURL, "/") + "/embeddings"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.EmbedAPIKey)
	res, err := c.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	text, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, statusError("embedding", res.StatusCode, text)
	}
	var parsed struct {
		Data []struct {
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.Unmarshal(text, &parsed); err != nil {
		return nil, err
	}
	out := make([][]float32, len(inputs))
	seen := 0
	for _, item := range parsed.Data {
		if item.Index < 0 || item.Index >= len(out) || len(item.Embedding) == 0 {
			return nil, fmt.Errorf("embedding index %d", item.Index)
		}
		out[item.Index] = item.Embedding
		seen++
	}
	if seen != len(inputs) {
		return nil, fmt.Errorf("embedding count %d, want %d", seen, len(inputs))
	}
	return out, nil
}

func extractPrompt(body string) string {
	return "从下面的提示词原文抽出 JSON，不要解释。keywords 是找回这一条用的说法，temperament 是气质词。" +
		"kind 只能是 style、medium、composition、lighting、genre。phrase 必须是原文里的连续子串。" +
		"spellings 是同一意思的其他写法。不要把分辨率、时长、帧率放进 temperament。" +
		"格式 {\"keywords\":[{\"phrase\":\"\"}],\"temperament\":[{\"kind\":\"composition\",\"phrase\":\"\",\"spellings\":[]}]}\n原文：\n" + body
}

func statusError(kind string, status int, body []byte) error {
	detail := strings.Join(strings.Fields(string(body)), " ")
	detail = clipText(detail, 500)
	if detail == "" {
		return fmt.Errorf("%s status %d", kind, status)
	}
	return fmt.Errorf("%s status %d: %s", kind, status, detail)
}

func clipText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for len(s) > 0 && !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}

func completionText(raw []byte) ([]byte, error) {
	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, err
	}
	for _, choice := range parsed.Choices {
		if strings.TrimSpace(choice.Message.Content) != "" {
			return []byte(choice.Message.Content), nil
		}
	}
	return nil, errors.New("term model returned no text")
}
