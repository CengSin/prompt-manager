package web

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"prompt-manager/internal/derive"
	"prompt-manager/internal/prompt"
	"prompt-manager/internal/store"
)

func newApp(t *testing.T) (*httptest.Server, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "prompts.db"))
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(NewHandler(st))
	t.Cleanup(func() {
		ts.Close()
		st.Close()
	})
	return ts, st
}

func postForm(t *testing.T, client *http.Client, endpoint, origin, referer string, form url.Values) (int, string, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	text := readBody(t, res.Body)
	return res.StatusCode, res.Request.URL.Path, text
}

func getPath(t *testing.T, client *http.Client, endpoint string) (int, string) {
	t.Helper()
	res, err := client.Get(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	return res.StatusCode, readBody(t, res.Body)
}

func readBody(t *testing.T, r io.Reader) string {
	t.Helper()
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestListenAddr(t *testing.T) {
	if got := ListenAddr(""); got != "127.0.0.1:8787" {
		t.Fatalf("default addr = %s", got)
	}
	if got := ListenAddr("9090"); got != "127.0.0.1:9090" {
		t.Fatalf("custom addr = %s", got)
	}
}

func TestOriginGuard(t *testing.T) {
	ts, st := newApp(t)
	client := ts.Client()

	status, _, _ := postForm(t, client, ts.URL+"/prompts", "https://evil.example", "", url.Values{
		"body": {"from elsewhere"},
	})
	if status != http.StatusForbidden {
		t.Fatalf("cross-origin status = %d", status)
	}
	assertCount(t, st, 0)

	status, path, page := postForm(t, client, ts.URL+"/prompts", ts.URL, "", url.Values{
		"body": {"from here"},
	})
	if status != http.StatusOK || !strings.HasPrefix(path, "/prompts/") {
		t.Fatalf("same-origin status=%d path=%s", status, path)
	}
	if !strings.Contains(page, "from here") {
		t.Fatalf("detail missing text: %s", page)
	}
	assertCount(t, st, 1)

	status, _, _ = postForm(t, client, ts.URL+"/prompts", "", ts.URL+"/prompts/new", url.Values{
		"body": {"from referer"},
	})
	if status != http.StatusOK {
		t.Fatalf("referer status = %d", status)
	}
	assertCount(t, st, 2)
}

func TestCreateListAndDetail(t *testing.T) {
	ts, st := newApp(t)
	client := ts.Client()
	status, page := getPath(t, client, ts.URL+"/")
	if status != http.StatusOK || !strings.Contains(page, "还没有提示词") {
		t.Fatalf("empty library status=%d page=%s", status, page)
	}

	full := "alpha\n  beta"
	status, path, page := postForm(t, client, ts.URL+"/prompts", ts.URL, "", url.Values{
		"body":        {full},
		"example_url": {"https://example.com/work"},
	})
	if status != http.StatusOK {
		t.Fatalf("create status = %d", status)
	}
	if !strings.Contains(page, full) {
		t.Fatalf("detail missing full text: %s", page)
	}
	if !strings.Contains(page, `href="https://example.com/work"`) {
		t.Fatalf("detail missing href: %s", page)
	}

	_, _, bare := postForm(t, client, ts.URL+"/prompts", ts.URL, "", url.Values{
		"body": {"no link here"},
	})
	if strings.Contains(bare, `href=""`) || strings.Contains(bare, "当初的作品") {
		t.Fatalf("empty link presented: %s", bare)
	}

	status, _, rejected := postForm(t, client, ts.URL+"/prompts", ts.URL, "", url.Values{
		"body":        {"keep this paste"},
		"example_url": {"javascript:alert(1)"},
	})
	if status != http.StatusBadRequest || !strings.Contains(rejected, "keep this paste") || !strings.Contains(rejected, "javascript:alert(1)") {
		t.Fatalf("rejected form status=%d page=%s", status, rejected)
	}
	if !strings.Contains(rejected, "example URL 必须是 http 或 https 链接") {
		t.Fatalf("missing url error: %s", rejected)
	}
	if _, err := st.Get(strings.TrimPrefix(path, "/prompts/")); err != nil {
		t.Fatal(err)
	}

	status, _, empty := postForm(t, client, ts.URL+"/prompts", ts.URL, "", url.Values{
		"body": {"   "},
	})
	if status != http.StatusBadRequest || !strings.Contains(empty, "原文必填") {
		t.Fatalf("empty text status=%d page=%s", status, empty)
	}
}

func TestSearchPage(t *testing.T) {
	ts, _ := newApp(t)
	client := ts.Client()
	postForm(t, client, ts.URL+"/prompts", ts.URL, "", url.Values{
		"body":        {"剪纸 Paper-cut"},
		"example_url": {"https://example.com/only-in-url"},
	})
	postForm(t, client, ts.URL+"/prompts", ts.URL, "", url.Values{
		"body": {"另一条也有剪纸"},
	})

	_, chinese := getPath(t, client, ts.URL+"/?q="+url.QueryEscape("剪纸"))
	if !strings.Contains(chinese, "<mark>剪纸</mark>") || !strings.Contains(chinese, "Paper-cut") || !strings.Contains(chinese, "另一条也有") {
		t.Fatalf("chinese search: %s", chinese)
	}
	_, latin := getPath(t, client, ts.URL+"/?q=paper-cut")
	if !strings.Contains(latin, "Paper-cut") || strings.Contains(latin, "另一条也有剪纸") {
		t.Fatalf("latin search: %s", latin)
	}
	_, onlyURL := getPath(t, client, ts.URL+"/?q=only-in-url")
	if !strings.Contains(onlyURL, "没有匹配的提示词") || strings.Contains(onlyURL, "剪纸") {
		t.Fatalf("url-only search: %s", onlyURL)
	}
	_, blank := getPath(t, client, ts.URL+"/?q="+url.QueryEscape("  \n"))
	if strings.Contains(blank, "没有匹配的提示词") || strings.Contains(blank, "还没有提示词") || !strings.Contains(blank, "另一条也有剪纸") {
		t.Fatalf("blank search: %s", blank)
	}
	_, none := getPath(t, client, ts.URL+"/?q="+url.QueryEscape("没有这段"))
	if !strings.Contains(none, "没有匹配的提示词") {
		t.Fatalf("no match: %s", none)
	}
}

func TestEdit(t *testing.T) {
	ts, st := newApp(t)
	client := ts.Client()
	_, path, _ := postForm(t, client, ts.URL+"/prompts", ts.URL, "", url.Values{
		"body": {"original"},
	})
	id := strings.TrimPrefix(path, "/prompts/")

	status, updatedPath, page := postForm(t, client, ts.URL+"/prompts/"+id, ts.URL, "", url.Values{
		"body":        {"replaced text"},
		"example_url": {"https://example.com/added"},
	})
	if status != http.StatusOK || updatedPath != "/prompts/"+id || !strings.Contains(page, "replaced text") || !strings.Contains(page, `href="https://example.com/added"`) {
		t.Fatalf("update status=%d path=%s page=%s", status, updatedPath, page)
	}

	_, _, cleared := postForm(t, client, ts.URL+"/prompts/"+id, ts.URL, "", url.Values{
		"body":        {"replaced text"},
		"example_url": {"  "},
	})
	if strings.Contains(cleared, "https://example.com/added") || strings.Contains(cleared, "当初的作品") {
		t.Fatalf("url was not cleared: %s", cleared)
	}

	status, _, rejected := postForm(t, client, ts.URL+"/prompts/"+id, ts.URL, "", url.Values{
		"body": {"  "},
	})
	if status != http.StatusBadRequest || !strings.Contains(rejected, "原文必填") {
		t.Fatalf("empty edit status=%d page=%s", status, rejected)
	}
	got, err := st.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Body != "replaced text" || got.ExampleURL != "" {
		t.Fatalf("stored after rejected edit = %#v", got)
	}
}

func TestDeleteConfirm(t *testing.T) {
	ts, st := newApp(t)
	client := ts.Client()
	_, path, _ := postForm(t, client, ts.URL+"/prompts", ts.URL, "", url.Values{
		"body": {"please keep"},
	})
	id := strings.TrimPrefix(path, "/prompts/")

	status, page := getPath(t, client, ts.URL+"/prompts/"+id+"/delete")
	if status != http.StatusOK || !strings.Contains(page, "确认删除") || !strings.Contains(page, "取消") {
		t.Fatalf("confirm status=%d page=%s", status, page)
	}
	if _, err := st.Get(id); err != nil {
		t.Fatal(err)
	}
	status, page = getPath(t, client, ts.URL+"/prompts/"+id)
	if status != http.StatusOK || !strings.Contains(page, "please keep") {
		t.Fatalf("cancel target status=%d page=%s", status, page)
	}

	status, listPath, listPage := postForm(t, client, ts.URL+"/prompts/"+id+"/delete", ts.URL, "", nil)
	if status != http.StatusOK || listPath != "/" || strings.Contains(listPage, "please keep") {
		t.Fatalf("deleted list status=%d path=%s page=%s", status, listPath, listPage)
	}
	status, missing := getPath(t, client, ts.URL+"/prompts/"+id)
	if status != http.StatusNotFound || !strings.Contains(missing, "未找到这条提示词") {
		t.Fatalf("missing status=%d page=%s", status, missing)
	}
}

func TestLiteralTextAndNoFetch(t *testing.T) {
	var hits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(upstream.Close)

	ts, _ := newApp(t)
	client := ts.Client()
	raw := "<script>alert(1)</script>"
	_, path, detail := postForm(t, client, ts.URL+"/prompts", ts.URL, "", url.Values{
		"body":        {raw},
		"example_url": {upstream.URL},
	})
	_, list := getPath(t, client, ts.URL+"/")
	_, found := getPath(t, client, ts.URL+"/?q=alert")
	assertLiteral(t, detail)
	assertLiteral(t, list)
	if strings.Contains(strings.ToLower(found), "<script") || !strings.Contains(found, "&lt;script&gt;") || !strings.Contains(found, "&lt;/script&gt;") || !strings.Contains(found, "<mark>alert</mark>") {
		t.Fatalf("highlighted script was not literal: %s", found)
	}
	if hits.Load() != 0 {
		t.Fatalf("example URL was requested %d times", hits.Load())
	}
	if !strings.Contains(detail, `href="`+upstream.URL+`"`) {
		t.Fatalf("missing stored href in %s", detail)
	}
	if path == "" {
		t.Fatal("missing detail path")
	}
}

func TestSearchHighlightAndMeaning(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "prompts.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	app := New(st, &fakeDeriver{embedReady: true, vector: []float32{1, 0}, model: "m"})
	app.SetLimits(0.5, 5)
	ts := httptest.NewServer(app)
	t.Cleanup(ts.Close)
	client := ts.Client()

	body := strings.Repeat("甲", 200) + "海滩两次还有海滩"
	beach, err := st.Create(body, "")
	if err != nil {
		t.Fatal(err)
	}
	alias, err := st.Create("喜欢剪纸风格", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.CommitDerived(alias.ID, alias.Generation, derive.Result{Terms: []derive.Term{{
		Kind: "style", Phrase: "剪纸风格", Start: len("喜欢"), End: len("喜欢剪纸风格"), Spellings: []string{"paper-cut"},
	}}}); err != nil {
		t.Fatal(err)
	}
	high, err := st.Create("高分段落", "")
	if err != nil {
		t.Fatal(err)
	}
	low, err := st.Create("低分段落", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.CommitDerived(high.ID, high.Generation, derive.Result{Vectors: []derive.Vector{{
		Source: "section", End: len("高分段落"), Model: "m", Values: []float32{1, 0},
	}}}); err != nil {
		t.Fatal(err)
	}
	if err := st.CommitDerived(low.ID, low.Generation, derive.Result{Vectors: []derive.Vector{{
		Source: "section", End: len("低分段落"), Model: "m", Values: []float32{0.6, 0.8},
	}}}); err != nil {
		t.Fatal(err)
	}

	_, listed := getPath(t, client, ts.URL+"/?q="+url.QueryEscape("海滩"))
	if !strings.Contains(listed, "意思相近") || strings.Contains(listed, strings.Repeat("甲", 200)) || !strings.Contains(listed, "<mark>海滩</mark>") {
		t.Fatalf("beach list: %s", listed)
	}
	if !strings.Contains(listed, "/prompts/"+beach.ID+"?q=") {
		t.Fatalf("detail link missing query: %s", listed)
	}
	_, detail := getPath(t, client, ts.URL+"/prompts/"+beach.ID+"?q="+url.QueryEscape("海滩"))
	if strings.Count(detail, "<mark>海滩</mark>") != 2 {
		t.Fatalf("detail highlights: %s", detail)
	}
	_, plain := getPath(t, client, ts.URL+"/prompts/"+beach.ID)
	if strings.Contains(plain, "<mark>") {
		t.Fatalf("unfiltered detail highlighted: %s", plain)
	}

	_, aliasPage := getPath(t, client, ts.URL+"/prompts/"+alias.ID+"?q=paper-cut")
	if !strings.Contains(aliasPage, "<mark>剪纸风格</mark>") || strings.Contains(aliasPage, "paper-cut") {
		t.Fatalf("alias detail: %s", aliasPage)
	}

	_, meaning := getPath(t, client, ts.URL+"/?q="+url.QueryEscape("番茄炒蛋"))
	highAt := strings.Index(meaning, "高分段落")
	lowAt := strings.Index(meaning, "低分段落")
	if !strings.Contains(meaning, "意思相近") || highAt < 0 || lowAt < 0 || highAt > lowAt {
		t.Fatalf("meaning order: %s", meaning)
	}
	app.SetLimits(0.5, 1)
	_, capped := getPath(t, client, ts.URL+"/?q="+url.QueryEscape("番茄炒蛋"))
	if strings.Contains(capped, "低分段落") || !strings.Contains(capped, "高分段落") {
		t.Fatalf("capped meaning: %s", capped)
	}
	sectionBody := "AAAA\n\nBBBB完整的一段"
	sections := prompt.Sections(sectionBody)
	if len(sections) != 2 {
		t.Fatalf("sections = %#v", sections)
	}
	sectionPrompt, err := st.Create(sectionBody, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.CommitDerived(sectionPrompt.ID, sectionPrompt.Generation, derive.Result{Vectors: []derive.Vector{
		{Source: "section", Start: sections[0].Start, End: sections[0].End, Model: "m", Values: []float32{0, 1}},
		{Source: "section", Start: sections[1].Start, End: sections[1].End, Model: "m", Values: []float32{1, 0}},
	}}); err != nil {
		t.Fatal(err)
	}
	app.SetLimits(0.5, 5)
	_, sectionPage := getPath(t, client, ts.URL+"/prompts/"+sectionPrompt.ID+"?q="+url.QueryEscape("番茄炒蛋"))
	if !strings.Contains(sectionPage, "<mark>BBBB完整的一段</mark>") || strings.Contains(sectionPage, "<mark>AAAA</mark>") {
		t.Fatalf("section highlight: %s", sectionPage)
	}

	raw := "<script>alert(1)</script> 海滩"
	scripted, err := st.Create(raw, "")
	if err != nil {
		t.Fatal(err)
	}
	_, scriptPage := getPath(t, client, ts.URL+"/prompts/"+scripted.ID+"?q="+url.QueryEscape("海滩"))
	assertLiteral(t, scriptPage)
	if !strings.Contains(scriptPage, "<mark>海滩</mark>") {
		t.Fatalf("script page missing highlight: %s", scriptPage)
	}
}

func TestMixedLanguageSearchResultsAndOriginalHighlights(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "prompts.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	app := New(st, &fakeDeriver{embedReady: true, vector: []float32{1, 0}, model: "m"})
	app.SetLimits(0.5, 1)
	ts := httptest.NewServer(app)
	t.Cleanup(ts.Close)
	client := ts.Client()

	query := "雨夜霓虹街道"
	literal, err := st.Create(query, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.CommitDerived(literal.ID, literal.Generation, derive.Result{Vectors: []derive.Vector{{
		Source: "section", End: len(query), Model: "m", Values: []float32{1, 0},
	}}}); err != nil {
		t.Fatal(err)
	}
	foreignBody := "A neon-lit street in the rain <script>alert(1)</script>"
	foreign, err := st.Create(foreignBody, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.CommitDerived(foreign.ID, foreign.Generation, derive.Result{Vectors: []derive.Vector{{
		Source: "section", End: len(foreignBody), Model: "m", Values: []float32{1, 0},
	}}}); err != nil {
		t.Fatal(err)
	}

	_, page := getPath(t, client, ts.URL+"/?q="+url.QueryEscape(query))
	literalAt := strings.Index(page, "<mark>"+query+"</mark>")
	foreignAt := strings.Index(page, "A neon-lit street")
	if literalAt < 0 || foreignAt < 0 || literalAt > foreignAt || !strings.Contains(page, "意思相近") {
		t.Fatalf("mixed result order: %s", page)
	}
	if strings.Count(page, "/prompts/"+literal.ID+"?q=") != 1 || strings.Count(page, "/prompts/"+foreign.ID+"?q=") != 1 {
		t.Fatalf("duplicate or missing result: %s", page)
	}
	assertLiteral(t, page)
	_, detail := getPath(t, client, ts.URL+"/prompts/"+foreign.ID+"?q="+url.QueryEscape(query))
	assertLiteral(t, detail)
	if !strings.Contains(detail, "<mark>A neon-lit street in the rain &lt;script&gt;alert(1)&lt;/script&gt;</mark>") || strings.Contains(detail, "<mark>"+query+"</mark>") {
		t.Fatalf("foreign original highlight: %s", detail)
	}
}

func TestFailurePageAndRetry(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "prompts.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	idle := New(st, &fakeDeriver{})
	ts := httptest.NewServer(idle)
	client := ts.Client()
	status, _, _ := postForm(t, client, ts.URL+"/prompts", ts.URL, "", url.Values{"body": {"尚未配置"}})
	if status != http.StatusOK {
		t.Fatalf("create status = %d", status)
	}
	got, err := st.List()
	if err != nil || len(got) != 1 || got[0].Status != store.StatusNone {
		t.Fatalf("unconfigured status = %#v err=%v", got, err)
	}
	_, failedPage := getPath(t, client, ts.URL+"/prompts/failed")
	if !strings.Contains(failedPage, "没有处理失败") || strings.Contains(failedPage, "尚未配置") {
		t.Fatalf("unconfigured failure page: %s", failedPage)
	}
	_, library := getPath(t, client, ts.URL+"/")
	if strings.Contains(library, "处理失败") {
		t.Fatalf("library linked failures: %s", library)
	}
	ts.Close()

	started := make(chan struct{}, 1)
	release := make(chan struct{})
	blocked := &fakeDeriver{ready: true, embedReady: true, started: started, release: release, err: "token secret-key rejected"}
	blocked.failLeft.Store(1)
	app := New(st, blocked)
	app.Start()
	t.Cleanup(app.Stop)
	ts = httptest.NewServer(app)
	t.Cleanup(ts.Close)
	client = ts.Client()
	done := make(chan struct{})
	go func() {
		postForm(t, client, ts.URL+"/prompts", ts.URL, "", url.Values{"body": {"待处理的原文"}})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("save waited for derivation")
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("derivation did not start")
	}
	_, pending := getPath(t, client, ts.URL+"/prompts/failed")
	if strings.Contains(pending, "待处理的原文") || !strings.Contains(pending, `http-equiv="refresh" content="2"`) {
		t.Fatalf("pending page: %s", pending)
	}
	close(release)
	waitFor(t, func() bool {
		pageStatus, page := getPath(t, client, ts.URL+"/prompts/failed")
		return pageStatus == http.StatusOK && strings.Contains(page, "待处理的原文") && strings.Contains(page, "token  rejected") && !strings.Contains(page, "secret-key")
	})
	_, library = getPath(t, client, ts.URL+"/")
	if !strings.Contains(library, "1 条处理失败") {
		t.Fatalf("library failure link: %s", library)
	}

	blocked.err = ""
	blocked.release = nil
	blocked.failLeft.Store(0)
	status, _, _ = postForm(t, client, ts.URL+"/prompts/failed/retry", "https://evil.example", "", nil)
	if status != http.StatusForbidden {
		t.Fatalf("cross-origin retry = %d", status)
	}
	status, path, _ := postForm(t, client, ts.URL+"/prompts/failed/retry", ts.URL, "", nil)
	if status != http.StatusOK || path != "/prompts/failed" {
		t.Fatalf("retry all status=%d path=%s", status, path)
	}
	waitFor(t, func() bool {
		items, err := st.List()
		if err != nil {
			return false
		}
		for _, item := range items {
			if strings.Contains(item.Body, "待处理") {
				return item.Status == store.StatusReady
			}
		}
		return false
	})
	_, page := getPath(t, client, ts.URL+"/prompts/failed")
	if strings.Contains(page, "待处理的原文") {
		t.Fatalf("successful retry still listed: %s", page)
	}

	again, err := st.Create("再失败一次", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.MarkFailed(again.ID, again.Generation, "旧原因"); err != nil {
		t.Fatal(err)
	}
	blocked.err = "新的失败"
	blocked.failLeft.Store(1)
	blocked.release = make(chan struct{})
	status, _, page = postForm(t, client, ts.URL+"/prompts/"+again.ID+"/derive", ts.URL, "", nil)
	if status != http.StatusOK {
		t.Fatalf("retry one status = %d", status)
	}
	if strings.Contains(page, "再失败一次") || !strings.Contains(page, `http-equiv="refresh" content="2"`) || !strings.Contains(page, "正在重跑") {
		t.Fatalf("retry page did not stay open for the result: %s", page)
	}
	close(blocked.release)
	waitFor(t, func() bool {
		_, page = getPath(t, client, ts.URL+"/prompts/failed")
		return strings.Contains(page, "新的失败") && strings.Contains(page, "再失败一次") && !strings.Contains(page, "旧原因") && strings.Contains(page, "2026") && !strings.Contains(page, "http-equiv=\"refresh\"")
	})

	if err := st.Delete(again.ID); err != nil {
		t.Fatal(err)
	}
	_, page = getPath(t, client, ts.URL+"/prompts/failed")
	if strings.Contains(page, "再失败一次") {
		t.Fatalf("deleted prompt still listed: %s", page)
	}
}

type fakeDeriver struct {
	ready      bool
	embedReady bool
	vector     []float32
	model      string
	err        string
	failLeft   atomic.Int32
	release    chan struct{}
	started    chan struct{}
	calls      atomic.Int32
}

func (f *fakeDeriver) Ready() bool      { return f.ready }
func (f *fakeDeriver) EmbedReady() bool { return f.embedReady }

func (f *fakeDeriver) Derive(ctx context.Context, body string) (derive.Result, error) {
	if f.started != nil {
		select {
		case f.started <- struct{}{}:
		default:
		}
	}
	if f.release != nil {
		select {
		case <-f.release:
		case <-ctx.Done():
			return derive.Result{}, ctx.Err()
		}
	}
	f.calls.Add(1)
	if f.err != "" && f.failLeft.Add(-1) >= 0 {
		return derive.Result{}, errors.New(f.err)
	}
	phrase := body
	if idx := strings.Index(body, "待处理"); idx >= 0 {
		phrase = "待处理"
	}
	return derive.Result{Terms: []derive.Term{{Kind: "keyword", Phrase: phrase, End: len(phrase)}}}, nil
}

func (f *fakeDeriver) EmbedQuery(context.Context, string) ([]float32, string, error) {
	if !f.embedReady {
		return nil, "", derive.ErrNotConfigured
	}
	return f.vector, f.model, nil
}

func (f *fakeDeriver) Redact(reason string) string {
	return strings.ReplaceAll(reason, "secret-key", "")
}

func waitFor(t *testing.T, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if ready() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("condition not met")
}

func TestMissingTermModelStaysIdle(t *testing.T) {
	t.Setenv("XAI_API_KEY", "from-env")
	t.Setenv("PROMPT_MANAGER_DERIVE_MODEL", "grok-4.7")
	t.Setenv("PROMPT_MANAGER_EMBED_API_KEY", "from-env")
	t.Setenv("PROMPT_MANAGER_EMBED_MODEL", "from-env")
	hits := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		hits++
	}))
	t.Cleanup(upstream.Close)
	path := filepath.Join(t.TempDir(), "config.json")
	raw := `{
		"term": {"baseUrl": "` + upstream.URL + `", "apiKey": "term-key"},
		"embed": {"baseUrl": "` + upstream.URL + `", "apiKey": "embed-key", "model": "vendor/embed"}
	}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := derive.LoadFile(path)
	if cfg.Ready() {
		t.Fatal("ready without term model")
	}
	st, err := store.Open(filepath.Join(t.TempDir(), "prompts.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	app := New(st, cfg)
	app.Start()
	t.Cleanup(app.Stop)
	ts := httptest.NewServer(app)
	t.Cleanup(ts.Close)
	status, _, _ := postForm(t, ts.Client(), ts.URL+"/prompts", ts.URL, "", url.Values{"body": {"缺词模型的原文"}})
	if status != http.StatusOK {
		t.Fatalf("create status = %d", status)
	}
	got, err := st.List()
	if err != nil || len(got) != 1 || got[0].Status != store.StatusNone {
		t.Fatalf("status = %#v err=%v", got, err)
	}
	_, failedPage := getPath(t, ts.Client(), ts.URL+"/prompts/failed")
	if strings.Contains(failedPage, "缺词模型的原文") {
		t.Fatalf("missing model listed as failed: %s", failedPage)
	}
	if hits != 0 {
		t.Fatalf("requests = %d", hits)
	}
}

func TestBackfillSkipsFailures(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "prompts.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	failed, err := st.Create("失败项", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.MarkFailed(failed.ID, failed.Generation, "先失败"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Create("待补算的原文", ""); err != nil {
		t.Fatal(err)
	}
	app := New(st, &fakeDeriver{ready: true, embedReady: true})
	app.Start()
	t.Cleanup(app.Stop)
	app.Backfill()
	waitFor(t, func() bool {
		items, err := st.List()
		if err != nil {
			return false
		}
		var idleReady, failedStays bool
		for _, item := range items {
			if item.Body == "待补算的原文" && item.Status == store.StatusReady {
				idleReady = true
			}
			if item.ID == failed.ID && item.Status == store.StatusFailed {
				failedStays = true
			}
		}
		return idleReady && failedStays
	})
}

func assertLiteral(t *testing.T, page string) {
	t.Helper()
	if strings.Contains(strings.ToLower(page), "<script") {
		t.Fatalf("executable script in page: %s", page)
	}
	if !strings.Contains(page, "&lt;script&gt;alert(1)&lt;/script&gt;") {
		t.Fatalf("script text not shown literally: %s", page)
	}
}

func assertCount(t *testing.T, st *store.Store, want int) {
	t.Helper()
	items, err := st.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != want {
		t.Fatalf("count = %d, want %d", len(items), want)
	}
}
