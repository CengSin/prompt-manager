package derive

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadFileUsesConfiguredEndpoints(t *testing.T) {
	var termHits, embedHits int
	var termBody, embedBody, termPath, embedPath string
	mux := http.NewServeMux()
	mux.HandleFunc("/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		termHits++
		termPath = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		termBody = string(raw)
		if r.Header.Get("Authorization") != "Bearer term-key" {
			t.Errorf("term auth = %s", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"{\"keywords\":[{\"phrase\":\"海滩\"}],\"temperament\":[]}"}}]}`)
	})
	mux.HandleFunc("/embeddings", func(w http.ResponseWriter, r *http.Request) {
		embedHits++
		embedPath = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		embedBody = string(raw)
		if r.Header.Get("Authorization") != "Bearer embed-key" {
			t.Errorf("embed auth = %s", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[{"index":0,"embedding":[1,0]}]}`)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	path := filepath.Join(t.TempDir(), "config.json")
	raw := `{
		"term": {"baseUrl": "` + server.URL + `", "apiKey": "term-key", "model": "vendor/term"},
		"embed": {"baseUrl": "` + server.URL + `", "apiKey": "embed-key", "model": "vendor/embed"},
		"similarityMin": 0.2,
		"meaningLimit": 3
	}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := LoadFile(path)
	cfg.client = server.Client()
	if cfg.SimilarityMin != 0.2 || cfg.MeaningLimit != 3 {
		t.Fatalf("limits = %v %d", cfg.SimilarityMin, cfg.MeaningLimit)
	}
	terms, err := cfg.Extract(context.Background(), "海边的海滩")
	if err != nil {
		t.Fatal(err)
	}
	if len(terms) != 1 || terms[0].Phrase != "海滩" {
		t.Fatalf("terms = %#v", terms)
	}
	if termPath != "/chat/completions" {
		t.Fatalf("term path = %s", termPath)
	}
	if !strings.Contains(termBody, "海边的海滩") || !strings.Contains(termBody, `"model":"vendor/term"`) || strings.Contains(termBody, "https://example.com/secret") {
		t.Fatalf("term request = %s", termBody)
	}
	vectors, err := cfg.Embed(context.Background(), []string{"海边的海滩"})
	if err != nil {
		t.Fatal(err)
	}
	if len(vectors) != 1 || len(vectors[0]) != 2 || embedPath != "/embeddings" {
		t.Fatalf("vectors = %#v path=%s", vectors, embedPath)
	}
	if !strings.Contains(embedBody, `"model":"vendor/embed"`) {
		t.Fatalf("embed request = %s", embedBody)
	}
	if termHits != 1 || embedHits != 1 {
		t.Fatalf("hits term=%d embed=%d", termHits, embedHits)
	}
}

func TestStatusErrorKeepsProviderBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"error":{"message":"key term-key is not allowed for this model","code":403}}`)
	}))
	defer server.Close()
	cfg := Config{
		TermBaseURL:  server.URL,
		TermKey:      "term-key",
		TermModel:    "vendor/term",
		EmbedBaseURL: server.URL,
		EmbedAPIKey:  "embed-key",
		EmbedModel:   "vendor/embed",
		client:       server.Client(),
	}
	_, err := cfg.Extract(context.Background(), "海滩")
	if err == nil || !strings.Contains(err.Error(), "term model status 403") || !strings.Contains(err.Error(), "not allowed for this model") {
		t.Fatalf("extract err = %v", err)
	}
	reason := cfg.Redact(err.Error())
	if strings.Contains(reason, "term-key") || !strings.Contains(reason, "status 403") {
		t.Fatalf("redacted = %s", reason)
	}
	_, err = cfg.Embed(context.Background(), []string{"海滩"})
	if err == nil || !strings.Contains(err.Error(), "embedding status 403") || !strings.Contains(err.Error(), "not allowed for this model") {
		t.Fatalf("embed err = %v", err)
	}
}

func TestLoadFileDefaultsAndSkipsMissingModel(t *testing.T) {
	hits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	path := filepath.Join(t.TempDir(), "config.json")
	raw := `{
		"term": {"apiKey": "term-key", "model": "vendor/term"},
		"embed": {"baseUrl": "` + server.URL + `/v1", "apiKey": "embed-key"}
	}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := LoadFile(path)
	cfg.client = server.Client()
	if cfg.TermBaseURL != defaultBase {
		t.Fatalf("term base = %s", cfg.TermBaseURL)
	}
	if cfg.EmbedReady() || cfg.Ready() {
		t.Fatal("embed ready without model")
	}
	if _, err := cfg.Embed(context.Background(), []string{"海滩"}); err != ErrNotConfigured {
		t.Fatalf("embed err = %v", err)
	}
	if _, err := cfg.Derive(context.Background(), "海滩"); err != ErrNotConfigured {
		t.Fatalf("derive err = %v", err)
	}
	if hits != 0 {
		t.Fatalf("requests = %d", hits)
	}
	if cfg.SimilarityMin != defaultSimilarity || cfg.MeaningLimit != defaultMeaningLimit {
		t.Fatalf("default limits = %v %d", cfg.SimilarityMin, cfg.MeaningLimit)
	}
}

func TestLoadFileIgnoresEnvironment(t *testing.T) {
	t.Setenv("XAI_API_KEY", "env-term")
	t.Setenv("PROMPT_MANAGER_DERIVE_MODEL", "grok-4.7")
	t.Setenv("PROMPT_MANAGER_EMBED_BASE_URL", "http://example.invalid/v1")
	t.Setenv("PROMPT_MANAGER_EMBED_API_KEY", "env-embed")
	t.Setenv("PROMPT_MANAGER_EMBED_MODEL", "vendor/embed")
	t.Setenv("PROMPT_MANAGER_SIMILARITY_MIN", "0.9")
	t.Setenv("PROMPT_MANAGER_MEANING_LIMIT", "9")

	hits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
	}))
	defer server.Close()

	cfg := LoadFile(filepath.Join(t.TempDir(), "missing.json"))
	cfg.client = server.Client()
	if cfg.Ready() || cfg.TermsReady() || cfg.EmbedReady() {
		t.Fatalf("env configured %#v", cfg)
	}
	if cfg.TermBaseURL != defaultBase || cfg.EmbedBaseURL != defaultBase || cfg.TermModel != "" || cfg.EmbedModel != "" {
		t.Fatalf("config = %#v", cfg)
	}
	if cfg.SimilarityMin != defaultSimilarity || cfg.MeaningLimit != defaultMeaningLimit {
		t.Fatalf("limits followed env: %v %d", cfg.SimilarityMin, cfg.MeaningLimit)
	}
	if _, err := cfg.Extract(context.Background(), "海滩"); err != ErrNotConfigured {
		t.Fatalf("extract err = %v", err)
	}
	if _, err := cfg.Embed(context.Background(), []string{"海滩"}); err != ErrNotConfigured {
		t.Fatalf("embed err = %v", err)
	}
	broken := filepath.Join(t.TempDir(), "broken.json")
	if err := os.WriteFile(broken, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if bad := LoadFile(broken); bad.Ready() || bad.TermKey != "" {
		t.Fatalf("broken file = %#v", bad)
	}
	if hits != 0 {
		t.Fatalf("requests = %d", hits)
	}
}

func TestLoadShortQueryMinimum(t *testing.T) {
	for _, tc := range []struct {
		name, raw string
		want      float64
	}{
		{"missing", `{}`, 0.60}, {"custom", `{"shortQuerySimilarityMin":0.65}`, 0.65}, {"zero", `{"shortQuerySimilarityMin":0}`, 0}, {"negative", `{"shortQuerySimilarityMin":-0.1}`, 0.60}, {"too high", `{"shortQuerySimilarityMin":1.1}`, 0.60},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(path, []byte(tc.raw), 0600); err != nil {
				t.Fatal(err)
			}
			if got := LoadFile(path).ShortQuerySimilarityMin; got != tc.want {
				t.Fatalf("minimum=%v want=%v", got, tc.want)
			}
		})
	}
}
