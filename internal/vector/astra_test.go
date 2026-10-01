package vector

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"prompt-manager/internal/derive"
)

func testAstra(t *testing.T, handler http.HandlerFunc) *Astra {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	a, err := New(Config{APIEndpoint: server.URL, ApplicationToken: "test-token"})
	if err != nil {
		t.Fatal(err)
	}
	a.client = server.Client()
	return a
}

func TestReplaceDeleteAndDimension(t *testing.T) {
	var calls []string
	deletes := 0
	a := testAstra(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Token") != "test-token" {
			t.Error("missing token")
		}
		var cmd map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&cmd); err != nil {
			t.Fatal(err)
		}
		for name, raw := range cmd {
			calls = append(calls, name)
			switch name {
			case "createCollection":
				if r.URL.Path != "/api/json/v1/default_keyspace" || !strings.Contains(string(raw), `"dimension":2`) || !strings.Contains(string(raw), `"metric":"cosine"`) {
					t.Errorf("create: %s %s", r.URL.Path, raw)
				}
			case "deleteMany":
				deletes++
				if deletes == 1 {
					w.Write([]byte(`{"status":{"moreData":true}}`))
					return
				}
			case "insertOne":
				if !strings.Contains(string(raw), `"$vector":[1,0]`) || !strings.Contains(string(raw), `"revision":"p:2"`) {
					t.Errorf("insert: %s", raw)
				}
			}
		}
		w.Write([]byte(`{"status":{"ok":1}}`))
	})
	ctx := context.Background()
	if err := a.Replace(ctx, Revision{ID: "p", Generation: 2}, []derive.Vector{{Source: "section", Model: "m", Start: 0, End: 4, Values: []float32{1, 0}}}); err != nil {
		t.Fatal(err)
	}
	if strings.Join(calls, ",") != "createCollection,deleteMany,deleteMany,insertOne" {
		t.Fatal(calls)
	}
	if err := a.Replace(ctx, Revision{ID: "p"}, []derive.Vector{{Values: []float32{1, 0, 0}}}); err == nil {
		t.Fatal("accepted dimension mismatch")
	}
}

func TestSearchDeduplicatesAndRefills(t *testing.T) {
	finds := 0
	a := testAstra(t, func(w http.ResponseWriter, r *http.Request) {
		var cmd map[string]json.RawMessage
		json.NewDecoder(r.Body).Decode(&cmd)
		if raw, ok := cmd["find"]; ok {
			finds++
			var query struct {
				Filter struct {
					Revision struct {
						In []string `json:"$in"`
					} `json:"revision"`
					Model string `json:"model"`
				} `json:"filter"`
			}
			json.Unmarshal(raw, &query)
			if query.Filter.Model != "m" {
				t.Error("model missing")
			}
			if finds == 1 {
				if len(query.Filter.Revision.In) != 3 {
					t.Fatal(query)
				}
				docs := make([]map[string]any, 1000)
				for i := range docs {
					docs[i] = map[string]any{"promptId": "a", "generation": 0, "revision": "a:0", "start": 0, "end": 2, "$similarity": 0.95}
				}
				json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"documents": docs}})
				return
			}
			if len(query.Filter.Revision.In) != 2 || query.Filter.Revision.In[0] != "b:0" {
				t.Fatal(query)
			}
			w.Write([]byte(`{"data":{"documents":[{"promptId":"b","generation":0,"revision":"b:0","start":2,"end":4,"$similarity":0.8},{"promptId":"c","generation":0,"revision":"c:0","$similarity":0.5}]}}`))
			return
		}
		w.Write([]byte(`{"status":{"ok":1}}`))
	})
	hits, err := a.Search(context.Background(), []Revision{{ID: "a"}, {ID: "b"}, {ID: "c"}}, []float32{1, 0}, "m", 0.35, 2)
	if err != nil || len(hits) != 2 || hits[0].PromptID != "a" || hits[1].PromptID != "b" || hits[1].Score < 0.59 || hits[1].Score > 0.61 || finds != 2 {
		t.Fatalf("%#v %v finds=%d", hits, err, finds)
	}
}

func TestErrorsAndConfiguration(t *testing.T) {
	a := testAstra(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"errors":[{"errorCode":"UNAUTHORIZED_ACCESS","message":"test-token"}]}`))
	})
	err := a.Replace(context.Background(), Revision{ID: "p"}, []derive.Vector{{Values: []float32{1, 0}}})
	if err == nil || !strings.Contains(err.Error(), "UNAUTHORIZED_ACCESS") || strings.Contains(err.Error(), "test-token") {
		t.Fatal(err)
	}
	empty, _ := New(Config{})
	if _, err := empty.Search(context.Background(), []Revision{{ID: "p"}}, []float32{1, 0}, "m", 0, 1); !errors.Is(err, ErrNotConfigured) {
		t.Fatal(err)
	}
	for _, endpoint := range []string{"https://astra.datastax.com/org/id/database", "http://localhost", "https://user:pass@example.com", "https://example.com?token=secret"} {
		if _, err := New(Config{APIEndpoint: endpoint}); err == nil {
			t.Fatal(endpoint)
		}
	}
	path := filepath.Join(t.TempDir(), "config.json")
	os.WriteFile(path, []byte(`{"astra":{"apiEndpoint":"https://example.com","applicationToken":"secret"},"embed":{"model":"m"}}`), 0600)
	cfg, err := LoadFile(path)
	if err != nil || !cfg.Ready() {
		t.Fatal(cfg, err)
	}
}

func TestDeleteMissingCollection(t *testing.T) {
	a := testAstra(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"errors":[{"errorCode":"UNKNOWN_COLLECTION_OR_TABLE"}]}`))
	})
	if err := a.Delete(context.Background(), "p"); err != nil {
		t.Fatal(err)
	}
}

func TestSearchDoesNotRefetchExhaustedResults(t *testing.T) {
	finds := 0
	a := testAstra(t, func(w http.ResponseWriter, r *http.Request) {
		var cmd map[string]json.RawMessage
		json.NewDecoder(r.Body).Decode(&cmd)
		if _, ok := cmd["find"]; ok {
			finds++
			w.Write([]byte(`{"data":{"documents":[{"promptId":"a","generation":0,"revision":"a:0","$similarity":0.83},{"promptId":"b","generation":0,"revision":"b:0","$similarity":0.78},{"promptId":"c","generation":0,"revision":"c:0","$similarity":0.7}]}}`))
			return
		}
		w.Write([]byte(`{"status":{"ok":1}}`))
	})
	hits, err := a.Search(context.Background(), []Revision{{ID: "a"}, {ID: "b"}, {ID: "c"}}, []float32{1, 0}, "m", 0.55, 5)
	if err != nil || len(hits) != 2 || finds != 1 {
		t.Fatalf("hits=%#v err=%v requests=%d", hits, err, finds)
	}
}
