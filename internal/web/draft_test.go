package web

import (
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"prompt-manager/internal/derive"
	"prompt-manager/internal/store"
)

func TestDraftRoundTrip(t *testing.T) {
	var hits atomic.Int32
	var chat atomic.Value
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		raw, _ := io.ReadAll(r.Body)
		switch r.URL.Path {
		case "/embeddings":
			if strings.Contains(string(raw), "嵌入失败") {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"data":[{"index":0,"embedding":[1,0]}]}`)
		case "/chat/completions":
			chat.Store(string(raw))
			if strings.Contains(string(raw), "泄露密钥") {
				w.WriteHeader(http.StatusForbidden)
				_, _ = io.WriteString(w, `{"error":"draft-key leaked"}`)
				return
			}
			if strings.Contains(string(raw), "空白稿") {
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"  \n  "}}]}`)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"生成的正文\n第二行"}}]}`)
		default:
			t.Errorf("unexpected %s", r.URL.Path)
		}
	}))
	t.Cleanup(upstream.Close)

	st, err := store.Open(t.TempDir() + "/prompts.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	body := "喜欢剪纸风格"
	item, err := st.Create(body, "https://example.com/do-not-send")
	if err != nil {
		t.Fatal(err)
	}
	y := math.Sqrt(0.75)
	if err := st.CommitDerived(item.ID, item.Generation, derive.Result{Vectors: []derive.Vector{{
		Source: "section", End: len(body), Model: "vendor/embed", Values: []float32{0.5, float32(y)},
	}}}); err != nil {
		t.Fatal(err)
	}
	cfg := derive.Config{
		DraftBaseURL: upstream.URL,
		DraftKey:     "draft-key",
		DraftModel:   "vendor/draft",
		EmbedBaseURL: upstream.URL,
		EmbedAPIKey:  "embed-key",
		EmbedModel:   "vendor/embed",
	}
	app := New(st, cfg)
	app.SetLimits(0.35, 5)
	app.SetShortQueryMinimum(0.90)
	ts := httptest.NewServer(app)
	t.Cleanup(ts.Close)
	client := ts.Client()

	status, page := getPath(t, client, ts.URL+"/prompts/draft")
	if status != http.StatusOK || !strings.Contains(page, "要写的内容") || strings.Contains(page, "未找到") {
		t.Fatalf("draft form status=%d page=%s", status, page)
	}
	before, err := st.List()
	if err != nil || len(before) != 1 {
		t.Fatal(err, len(before))
	}

	status, _, empty := postForm(t, client, ts.URL+"/prompts/draft", ts.URL, "", url.Values{"need": {"  "}})
	if status != http.StatusBadRequest || !strings.Contains(empty, "要写的内容必填") || hits.Load() != 0 {
		t.Fatalf("empty status=%d hits=%d page=%s", status, hits.Load(), empty)
	}

	bare := New(st, derive.Config{})
	bareServer := httptest.NewServer(bare)
	t.Cleanup(bareServer.Close)
	status, _, unconfigured := postForm(t, bareServer.Client(), bareServer.URL+"/prompts/draft", bareServer.URL, "", url.Values{"need": {"春节厨房"}})
	if !strings.Contains(unconfigured, "还不能生成") || hits.Load() != 0 {
		t.Fatalf("unconfigured status=%d hits=%d page=%s", status, hits.Load(), unconfigured)
	}

	status, _, drafted := postForm(t, client, ts.URL+"/prompts/draft", ts.URL, "", url.Values{"need": {"剪纸"}})
	if status != http.StatusOK || !strings.Contains(drafted, "生成的正文\n第二行") || !strings.Contains(drafted, "这是生成的草稿，改完再保存") || strings.Contains(drafted, "这次没有参考") {
		t.Fatalf("draft status=%d page=%s", status, drafted)
	}
	request, _ := chat.Load().(string)
	if !strings.Contains(request, "要写的内容") || !strings.Contains(request, "剪纸") || !strings.Contains(request, "写法范例") || !strings.Contains(request, body) || strings.Contains(request, "do-not-send") {
		t.Fatalf("writing request = %s", request)
	}
	after, err := st.List()
	if err != nil || len(after) != 1 {
		t.Fatalf("draft stored prompts: %v %d", err, len(after))
	}

	status, _, saved := postForm(t, client, ts.URL+"/prompts", ts.URL, "", url.Values{"body": {"改过的正文"}})
	if status != http.StatusOK || !strings.Contains(saved, "改过的正文") {
		t.Fatalf("save status=%d page=%s", status, saved)
	}
	stored, err := st.List()
	if err != nil || len(stored) != 2 {
		t.Fatalf("stored = %v %d", err, len(stored))
	}
	for _, prompt := range stored {
		if prompt.Body == "剪纸" {
			t.Fatal("stored the description")
		}
	}

	status, _, failedEmbed := postForm(t, client, ts.URL+"/prompts/draft", ts.URL, "", url.Values{"need": {"嵌入失败也要写"}})
	if status != http.StatusOK || !strings.Contains(failedEmbed, "这次没有参考") || !strings.Contains(failedEmbed, "生成的正文") {
		t.Fatalf("embed failure status=%d page=%s", status, failedEmbed)
	}
	count, err := st.List()
	if err != nil || len(count) != 2 {
		t.Fatalf("count after unsaved draft = %d %v", len(count), err)
	}

	status, _, blank := postForm(t, client, ts.URL+"/prompts/draft", ts.URL, "", url.Values{"need": {"空白稿"}})
	if !strings.Contains(blank, "没有生成出来") || !strings.Contains(blank, "空白稿") || strings.Contains(blank, "这是生成的草稿") {
		t.Fatalf("blank page=%s", blank)
	}
	status, _, leaked := postForm(t, client, ts.URL+"/prompts/draft", ts.URL, "", url.Values{"need": {"泄露密钥"}})
	if !strings.Contains(leaked, "没有生成出来") || strings.Contains(leaked, "draft-key") || !strings.Contains(leaked, "泄露密钥") {
		t.Fatalf("leaked status=%d page=%s", status, leaked)
	}
}
