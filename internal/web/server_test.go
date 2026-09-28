package web

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

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
	if !strings.Contains(chinese, "剪纸 Paper-cut") || !strings.Contains(chinese, "另一条也有剪纸") {
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
	assertLiteral(t, found)
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
