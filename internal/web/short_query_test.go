package web

import (
	"math"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"prompt-manager/internal/derive"
	"prompt-manager/internal/store"
)

func TestQueryMinimum(t *testing.T) {
	app := &App{min: 0.55, shortQueryMin: 0.60}
	for _, tc := range []struct {
		query string
		want  float64
	}{
		{"美女", 0.60}, {"  美女  ", 0.60}, {"海滩美女", 0.60}, {"海滩上的美女", 0.55}, {"girl", 0.60}, {"beautiful woman", 0.55}, {"", 0.55}, {"  ", 0.55},
	} {
		if got := app.queryMinimum(tc.query); got != tc.want {
			t.Errorf("query=%q minimum=%v want=%v", tc.query, got, tc.want)
		}
	}
	app.SetLimits(0.75, 5)
	if got := app.queryMinimum("美女"); got != 0.75 {
		t.Fatal("short query relaxed stricter configured minimum", got)
	}
	app.SetLimits(0.55, 5)
	app.SetShortQueryMinimum(0.65)
	if got := app.queryMinimum("美女"); got != 0.65 {
		t.Fatal("short minimum not configurable", got)
	}
}

func TestShortQueryFiltersWeakSemanticMatchAndKeepsLiteral(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "prompts.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	add := func(body string, score float64) store.Prompt {
		t.Helper()
		item, err := st.Create(body, "")
		if err != nil {
			t.Fatal(err)
		}
		result := derive.Result{Vectors: []derive.Vector{{Source: "term", Start: 0, End: len(body), Model: "m", Values: []float32{float32(score), float32(math.Sqrt(1 - score*score))}}}}
		if err := st.CommitDerived(item.ID, item.Generation, result); err != nil {
			t.Fatal(err)
		}
		return item
	}
	beach := add("海滩上的成年女性", 0.6649)
	game := add("太空游戏のニアミス", 0.5704)
	literal := add("美女字面命中", 0)
	app := New(st, &fakeDeriver{embedReady: true, vector: []float32{1, 0}, model: "m"})
	app.SetLimits(0.55, 5)
	ts := httptest.NewServer(app)
	defer ts.Close()
	_, short := getPath(t, ts.Client(), ts.URL+"/?q="+url.QueryEscape("美女"))
	if !strings.Contains(short, "/prompts/"+beach.ID+"?q=") || strings.Contains(short, "/prompts/"+game.ID+"?q=") || !strings.Contains(short, "/prompts/"+literal.ID+"?q=") {
		t.Fatalf("unexpected short query results: %s", short)
	}
	_, long := getPath(t, ts.Client(), ts.URL+"/?q="+url.QueryEscape("请帮我搜索美女"))
	if !strings.Contains(long, "/prompts/"+game.ID+"?q=") {
		t.Fatal("long query minimum changed")
	}
	_, detail := getPath(t, ts.Client(), ts.URL+"/prompts/"+game.ID+"?q="+url.QueryEscape("美女"))
	if strings.Contains(detail, "<mark>") {
		t.Fatal("detail highlighted excluded short query result")
	}
	_, beachDetail := getPath(t, ts.Client(), ts.URL+"/prompts/"+beach.ID+"?q="+url.QueryEscape("美女"))
	if !strings.Contains(beachDetail, "<mark>海滩上的成年女性</mark>") {
		t.Fatal("qualifying semantic highlight missing")
	}
}
