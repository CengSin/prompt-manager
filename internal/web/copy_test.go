package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"prompt-manager/internal/derive"
	"prompt-manager/internal/store"
)

func TestCopyScript(t *testing.T) {
	ts, _ := newApp(t)
	status, body := getPath(t, ts.Client(), ts.URL+"/copy.js")
	if status != 200 || strings.Contains(body, "innerHTML") || !strings.Contains(body, "navigator.clipboard.writeText") || !strings.Contains(body, "已复制") || !strings.Contains(body, "没复制成") {
		t.Fatalf("status=%d script=%s", status, body)
	}
}

func TestCopyAndAddedDate(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/prompts.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	st.SetNow(func() time.Time { return time.Date(2026, 9, 28, 16, 0, 0, 0, time.UTC) })
	lateBody := strings.Repeat("海", 130) + "海滩<script>alert(1)</script></textarea>"
	late, err := st.Create(lateBody, "https://example.com/keep-out")
	if err != nil {
		t.Fatal(err)
	}
	st.SetNow(func() time.Time { return time.Date(2026, 9, 28, 15, 59, 59, 0, time.UTC) })
	if _, err := st.Create("较早的提示词", ""); err != nil {
		t.Fatal(err)
	}
	meaningBody := "A neon-lit street"
	meaning, err := st.Create(meaningBody, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.CommitDerived(meaning.ID, meaning.Generation, derive.Result{Vectors: []derive.Vector{{
		Source: "section", End: len(meaningBody), Model: "m", Values: []float32{1, 0},
	}}}); err != nil {
		t.Fatal(err)
	}
	app := New(st, &fakeDeriver{embedReady: true, vector: []float32{1, 0}, model: "m"})
	app.SetLimits(0.35, 5)
	ts := httptest.NewServer(app)
	t.Cleanup(ts.Close)
	client := ts.Client()

	_, list := getPath(t, client, ts.URL+"/")
	if !strings.Contains(list, "写一条") || !strings.Contains(list, "添加于 2026-09-29") || !strings.Contains(list, "添加于 2026-09-28") || !strings.Contains(list, `class="count">3 条`) || !strings.Contains(list, `placeholder="原文、一个词，或一句意思"`) || !strings.Contains(list, `>example.com/keep-out</a>`) || !strings.Contains(list, `href="https://example.com/keep-out"`) {
		t.Fatalf("list dates: %s", list)
	}
	field := copyField(t, list, late.ID)
	if !strings.Contains(field, strings.Repeat("海", 130)) || !strings.Contains(field, "&lt;script&gt;alert(1)&lt;/script&gt;&lt;/textarea&gt;") || strings.Contains(field, "keep-out") {
		t.Fatalf("copy field = %s", field)
	}
	assertLiteral(t, list)

	_, detail := getPath(t, client, ts.URL+"/prompts/"+late.ID)
	if strings.Contains(detail, "添加于") {
		t.Fatalf("detail shows added date: %s", detail)
	}
	detailField := copyField(t, detail, late.ID)
	if detailField != field {
		t.Fatalf("detail copy = %s", detailField)
	}
	assertLiteral(t, detail)

	_, found := getPath(t, client, ts.URL+"/?q="+url.QueryEscape("海滩"))
	if !strings.Contains(found, "意思相近") || !strings.Contains(found, "添加于 2026-09-29") {
		t.Fatalf("search dates: %s", found)
	}
	if copyField(t, found, late.ID) != field || copyField(t, found, meaning.ID) != meaningBody {
		t.Fatalf("search copy fields: %s", found)
	}

	status, _, _ := postForm(t, client, ts.URL+"/prompts/"+late.ID, ts.URL, "", url.Values{
		"body": {"改正后的海滩全文"},
	})
	if status != http.StatusOK {
		t.Fatalf("correct status=%d", status)
	}
	_, corrected := getPath(t, client, ts.URL+"/")
	if !strings.Contains(corrected, "添加于 2026-09-29") || !strings.Contains(corrected, "改正后的海滩全文") {
		t.Fatalf("corrected list: %s", corrected)
	}
}

func TestExampleLabel(t *testing.T) {
	cases := []struct{ raw, want string }{
		{"https://x.com/riku720720/status/2102515055116063144", "x.com/riku720720"},
		{"https://twitter.com/ohmuyi/status/1?s=20", "x.com/ohmuyi"},
		{"https://example.com/keep-out", "example.com/keep-out"},
		{"https://example.com", "example.com"},
		{"not a url", "not a url"},
	}
	for _, tc := range cases {
		if got := exampleLabel(tc.raw); got != tc.want {
			t.Errorf("exampleLabel(%q) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}

func copyField(t *testing.T, page, id string) string {
	t.Helper()
	marker := `<textarea id="copy-` + id + `" hidden readonly>`
	_, rest, ok := strings.Cut(page, marker)
	if !ok {
		t.Fatalf("missing copy field %s in %s", id, page)
	}
	field, _, ok := strings.Cut(rest, "</textarea>")
	if !ok {
		t.Fatalf("unclosed copy field in %s", page)
	}
	return field
}
