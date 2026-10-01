package vector

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"prompt-manager/internal/derive"
)

var ErrNotConfigured = errors.New("Astra DB is not configured")
var errUnknownCollection = errors.New("Astra collection does not exist")

type Config struct {
	APIEndpoint      string `json:"apiEndpoint"`
	ApplicationToken string `json:"applicationToken"`
	Keyspace         string `json:"keyspace"`
	Collection       string `json:"collection"`
}

func LoadFile(path string) (Config, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Config{}, nil
	}
	if err != nil {
		return Config{}, err
	}
	var doc struct {
		Astra Config `json:"astra"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return Config{}, errors.New("invalid database configuration JSON")
	}
	return doc.Astra, nil
}

func (c Config) Ready() bool {
	return strings.TrimSpace(c.APIEndpoint) != "" && strings.TrimSpace(c.ApplicationToken) != ""
}

type Revision struct {
	ID         string
	Generation int
}

func (r Revision) Key() string { return fmt.Sprintf("%s:%d", r.ID, r.Generation) }

type Hit struct {
	PromptID   string   `json:"promptId"`
	Generation int      `json:"generation"`
	Revision   string   `json:"revision"`
	Start      int      `json:"start"`
	End        int      `json:"end"`
	Similarity *float64 `json:"$similarity"`
	Score      float64  `json:"-"`
}

type Backend interface {
	Replace(context.Context, Revision, []derive.Vector) error
	Delete(context.Context, string) error
	Search(context.Context, []Revision, []float32, string, float64, int) ([]Hit, error)
}

type Astra struct {
	cfg       Config
	client    *http.Client
	mu        sync.Mutex
	dimension int
}

func New(c Config) (*Astra, error) {
	c.APIEndpoint = strings.TrimRight(strings.TrimSpace(c.APIEndpoint), "/")
	c.ApplicationToken = strings.TrimSpace(c.ApplicationToken)
	c.Keyspace = strings.TrimSpace(c.Keyspace)
	c.Collection = strings.TrimSpace(c.Collection)
	if c.Keyspace == "" {
		c.Keyspace = "default_keyspace"
	}
	if c.Collection == "" {
		c.Collection = "prompt_vectors"
	}
	if c.APIEndpoint != "" {
		u, err := url.Parse(c.APIEndpoint)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
			return nil, errors.New("Astra apiEndpoint must be an HTTPS database endpoint without a path")
		}
	}
	return &Astra{cfg: c, client: &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

func (a *Astra) call(ctx context.Context, collection bool, command any, out any) error {
	if !a.cfg.Ready() {
		return ErrNotConfigured
	}
	raw, err := json.Marshal(command)
	if err != nil {
		return err
	}
	endpoint := a.cfg.APIEndpoint + "/api/json/v1/" + url.PathEscape(a.cfg.Keyspace)
	if collection {
		endpoint += "/" + url.PathEscape(a.cfg.Collection)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Token", a.cfg.ApplicationToken)
	req.Header.Set("Content-Type", "application/json")
	res, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("Astra request failed: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("Astra HTTP status %d", res.StatusCode)
	}
	var envelope struct {
		Data   json.RawMessage `json:"data"`
		Status json.RawMessage `json:"status"`
		Errors []struct {
			Code string `json:"errorCode"`
		} `json:"errors"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 8<<20)).Decode(&envelope); err != nil {
		return errors.New("invalid Astra response")
	}
	if len(envelope.Errors) > 0 {
		if envelope.Errors[0].Code == "UNKNOWN_COLLECTION_OR_TABLE" {
			return errUnknownCollection
		}
		code := strings.ReplaceAll(envelope.Errors[0].Code, a.cfg.ApplicationToken, "[redacted]")
		return fmt.Errorf("Astra Data API error: %s", code)
	}
	if out != nil {
		b, err := json.Marshal(envelope)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(b, out); err != nil {
			return errors.New("invalid Astra result")
		}
	}
	return nil
}

func (a *Astra) ensure(ctx context.Context, dimension int) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if dimension <= 0 {
		return errors.New("empty vector")
	}
	if a.dimension != 0 {
		if a.dimension != dimension {
			return fmt.Errorf("Astra vector dimension %d, received %d; use a new collection for a different embedding dimension", a.dimension, dimension)
		}
		return nil
	}
	err := a.call(ctx, false, map[string]any{"createCollection": map[string]any{"name": a.cfg.Collection, "options": map[string]any{"vector": map[string]any{"dimension": dimension, "metric": "cosine"}}}}, nil)
	if err == nil {
		a.dimension = dimension
	}
	return err
}

func (a *Astra) Delete(ctx context.Context, id string) error {
	if !a.cfg.Ready() {
		return nil
	}
	err := a.delete(ctx, map[string]any{"promptId": id})
	if errors.Is(err, errUnknownCollection) {
		return nil
	}
	return err
}

func (a *Astra) delete(ctx context.Context, filter any) error {
	for {
		var res struct {
			Status struct {
				More bool `json:"moreData"`
			} `json:"status"`
		}
		if err := a.call(ctx, true, map[string]any{"deleteMany": map[string]any{"filter": filter}}, &res); err != nil {
			return err
		}
		if !res.Status.More {
			return nil
		}
	}
}

func (a *Astra) Replace(ctx context.Context, rev Revision, vectors []derive.Vector) error {
	if !a.cfg.Ready() {
		return ErrNotConfigured
	}
	if len(vectors) == 0 {
		return a.Delete(ctx, rev.ID)
	}
	dim := len(vectors[0].Values)
	for _, v := range vectors {
		if len(v.Values) != dim {
			return errors.New("inconsistent vector dimensions")
		}
	}
	if err := a.ensure(ctx, dim); err != nil {
		return err
	}
	if err := a.delete(ctx, map[string]any{"promptId": rev.ID}); err != nil {
		return err
	}
	for i, v := range vectors {
		doc := map[string]any{"_id": fmt.Sprintf("%s:%d", rev.Key(), i), "promptId": rev.ID, "generation": rev.Generation, "revision": rev.Key(), "source": v.Source, "start": v.Start, "end": v.End, "model": v.Model, "dimension": dim, "$vector": v.Values}
		if err := a.call(ctx, true, map[string]any{"insertOne": map[string]any{"document": doc}}, nil); err != nil {
			return err
		}
	}
	return nil
}

func (a *Astra) Search(ctx context.Context, revisions []Revision, values []float32, model string, min float64, limit int) ([]Hit, error) {
	started := time.Now()
	requests := 0
	defer func() {
		if requests > 0 {
			log.Printf("Astra search: requests=%d duration=%s", requests, time.Since(started).Round(time.Millisecond))
		}
	}()
	if limit <= 0 || len(revisions) == 0 {
		return nil, nil
	}
	if err := a.ensure(ctx, len(values)); err != nil {
		return nil, err
	}
	var hits []Hit
	for start := 0; start < len(revisions); start += 50 {
		end := start + 50
		if end > len(revisions) {
			end = len(revisions)
		}
		keys := make([]string, 0, end-start)
		for _, r := range revisions[start:end] {
			keys = append(keys, r.Key())
		}
		count := 0
		for len(keys) > 0 && count < limit {
			var res struct {
				Data struct {
					Documents []Hit `json:"documents"`
				} `json:"data"`
			}
			filter := map[string]any{"revision": map[string]any{"$in": keys}, "model": model, "dimension": len(values)}
			requests++
			if err := a.call(ctx, true, map[string]any{"find": map[string]any{"filter": filter, "sort": map[string]any{"$vector": values}, "projection": map[string]any{"promptId": true, "generation": true, "revision": true, "start": true, "end": true}, "options": map[string]any{"limit": 1000, "includeSimilarity": true}}}, &res); err != nil {
				return nil, err
			}
			seen := map[string]bool{}
			eligible := map[string]bool{}
			for _, key := range keys {
				eligible[key] = true
			}
			for _, h := range res.Data.Documents {
				if !eligible[h.Revision] || seen[h.Revision] {
					continue
				}
				if h.Similarity == nil {
					return nil, errors.New("Astra vector result missing similarity")
				}
				h.Score = 2*(*h.Similarity) - 1
				if h.Score < min {
					break
				}
				seen[h.Revision] = true
				hits = append(hits, h)
				count++
				if count == limit {
					break
				}
			}
			if len(seen) == 0 {
				break
			}
			if len(res.Data.Documents) < 1000 {
				break
			}
			next := keys[:0]
			for _, key := range keys {
				if !seen[key] {
					next = append(next, key)
				}
			}
			keys = next
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits, nil
}
