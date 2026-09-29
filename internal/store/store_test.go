package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"prompt-manager/internal/derive"
	"prompt-manager/internal/prompt"

	_ "modernc.org/sqlite"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "nested", "prompts.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func setClock(s *Store, times ...time.Time) {
	i := 0
	s.now = func() time.Time {
		if i >= len(times) {
			return times[len(times)-1]
		}
		current := times[i]
		i++
		return current
	}
}

func TestOpenCreatesSchemaTwice(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prompts.db")
	first, err := Open(path)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := Open(path)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	defer second.Close()

	rows, err := second.db.Query(`SELECT name FROM pragma_table_info('prompts')`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	got := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		got[name] = true
	}
	for _, name := range []string{"id", "body", "body_search", "example_url", "created_at", "updated_at", "derive_generation", "derive_status", "derive_error", "derive_failed_at"} {
		if !got[name] {
			t.Fatalf("missing column %s in %#v", name, got)
		}
	}
	for _, table := range []string{"prompt_terms", "prompt_spellings", "prompt_vectors"} {
		var name string
		if err := second.db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&name); err != nil {
			t.Fatalf("table %s: %v", table, err)
		}
	}
}

func TestOpenMigratesExistingLibrary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prompts.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE prompts (
		id TEXT PRIMARY KEY,
		body TEXT NOT NULL,
		body_search TEXT NOT NULL,
		example_url TEXT,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO prompts (id, body, body_search, created_at, updated_at) VALUES ('old', '旧原文', '旧原文', '2026-01-01T00:00:00.000000000Z', '2026-01-01T00:00:00.000000000Z')`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	first, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	got, err := second.Get("old")
	if err != nil {
		t.Fatal(err)
	}
	if got.Body != "旧原文" || got.Status != StatusNone || got.Generation != 0 {
		t.Fatalf("migrated prompt = %#v", got)
	}
}

func TestCreateListAndGet(t *testing.T) {
	s := openTestStore(t)
	earlier := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	later := earlier.Add(time.Second)
	setClock(s, earlier, later)

	body := "  first\nline  "
	first, err := s.Create(body, "")
	if err != nil {
		t.Fatal(err)
	}
	if first.Body != body {
		t.Fatalf("body = %q", first.Body)
	}
	if first.ExampleURL != "" {
		t.Fatalf("url = %q", first.ExampleURL)
	}
	second, err := s.Create("second", "https://example.com/a")
	if err != nil {
		t.Fatal(err)
	}

	list, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].ID != second.ID || list[1].ID != first.ID {
		t.Fatalf("list order = %#v", ids(list))
	}

	got, err := s.Get(first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Body != body {
		t.Fatalf("got body = %q", got.Body)
	}
	if _, err := s.Get("missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing err = %v", err)
	}
}

func TestUpdateKeepsIdentityAndOrder(t *testing.T) {
	s := openTestStore(t)
	t0 := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	setClock(s, t0, t0.Add(time.Second), t0.Add(2*time.Second), t0.Add(3*time.Second))

	older, err := s.Create("older paper", "")
	if err != nil {
		t.Fatal(err)
	}
	newer, err := s.Create("newer", "")
	if err != nil {
		t.Fatal(err)
	}

	updated, err := s.Update(older.ID, "older rewritten", "https://example.com/new")
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != older.ID {
		t.Fatalf("id changed to %s", updated.ID)
	}
	if !updated.CreatedAt.Equal(older.CreatedAt) {
		t.Fatalf("created_at changed from %s to %s", older.CreatedAt, updated.CreatedAt)
	}
	if updated.ExampleURL != "https://example.com/new" {
		t.Fatalf("url = %q", updated.ExampleURL)
	}

	list, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if list[0].ID != newer.ID || list[1].ID != older.ID {
		t.Fatalf("order after update = %#v", ids(list))
	}

	cleared, err := s.Update(older.ID, "older rewritten", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if cleared.ExampleURL != "" {
		t.Fatalf("cleared url = %q", cleared.ExampleURL)
	}

	before := cleared
	if _, err := s.Update(older.ID, " \n ", "https://example.com/ignored"); !errors.Is(err, prompt.ErrBodyRequired) {
		t.Fatalf("empty update err = %v", err)
	}
	after, err := s.Get(older.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Body != before.Body || after.ExampleURL != before.ExampleURL || !after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Fatalf("invalid update changed record: before %#v after %#v", before, after)
	}

	found, err := s.Search("rewritten")
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || found[0].ID != older.ID {
		t.Fatalf("search after update = %#v", ids(found))
	}
}

func TestDeleteRemovesFromListSearchAndGet(t *testing.T) {
	s := openTestStore(t)
	item, err := s.Create("delete me 剪纸", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(item.ID); err != nil {
		t.Fatal(err)
	}
	list, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("list = %#v", list)
	}
	found, err := s.Search("剪纸")
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 0 {
		t.Fatalf("search = %#v", found)
	}
	if _, err := s.Get(item.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get err = %v", err)
	}
}

func TestSearchMatchesBodyOnly(t *testing.T) {
	s := openTestStore(t)
	t0 := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	setClock(s, t0, t0.Add(time.Second))
	older, err := s.Create("剪纸 Paper-cut", "https://example.com/only-in-url")
	if err != nil {
		t.Fatal(err)
	}
	newer, err := s.Create("另一条也有剪纸", "")
	if err != nil {
		t.Fatal(err)
	}

	chinese, err := s.Search("剪纸")
	if err != nil {
		t.Fatal(err)
	}
	if len(chinese) != 2 || chinese[0].ID != newer.ID || chinese[1].ID != older.ID {
		t.Fatalf("chinese = %#v", ids(chinese))
	}

	latin, err := s.Search("paper-cut")
	if err != nil {
		t.Fatal(err)
	}
	if len(latin) != 1 || latin[0].ID != older.ID {
		t.Fatalf("latin = %#v", ids(latin))
	}

	fromURL, err := s.Search("only-in-url")
	if err != nil {
		t.Fatal(err)
	}
	if len(fromURL) != 0 {
		t.Fatalf("url query matched %#v", fromURL)
	}

	none, err := s.Search("没有这段")
	if err != nil {
		t.Fatal(err)
	}
	if len(none) != 0 {
		t.Fatalf("none = %#v", none)
	}

	all, err := s.Search("  \n")
	if err != nil {
		t.Fatal(err)
	}
	list, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != len(list) || all[0].ID != list[0].ID || all[1].ID != list[1].ID {
		t.Fatalf("blank search = %#v list = %#v", ids(all), ids(list))
	}
}

func TestDerivedDataFollowsTextAndDelete(t *testing.T) {
	s := openTestStore(t)
	item, err := s.Create("剪纸风格的海滩", "https://example.com/keep")
	if err != nil {
		t.Fatal(err)
	}
	batch := derive.Result{
		Terms: []derive.Term{{
			Kind: "style", Phrase: "剪纸风格", Start: 0, End: len("剪纸风格"), Spellings: []string{"paper-cut"},
		}},
		Vectors: []derive.Vector{{
			Source: "term", Start: 0, End: len("剪纸风格"), Model: "m", Values: []float32{1, 0},
		}},
	}
	if err := s.CommitDerived(item.ID, item.Generation, batch); err != nil {
		t.Fatal(err)
	}
	kept, err := s.Update(item.ID, item.Body, "")
	if err != nil {
		t.Fatal(err)
	}
	if kept.Generation != item.Generation || kept.Status != StatusReady {
		t.Fatalf("url-only update = %#v", kept)
	}
	found, err := s.Search("paper-cut")
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || found[0].ID != item.ID {
		t.Fatalf("spelling search = %#v", ids(found))
	}
	rewritten, err := s.Update(item.ID, "完全不同的原文", "")
	if err != nil {
		t.Fatal(err)
	}
	if rewritten.Generation != item.Generation+1 || rewritten.Status != StatusNone {
		t.Fatalf("text update = %#v", rewritten)
	}
	if err := s.CommitDerived(item.ID, item.Generation, batch); !errors.Is(err, ErrStale) {
		t.Fatalf("stale commit err = %v", err)
	}
	stale, err := s.Search("paper-cut")
	if err != nil {
		t.Fatal(err)
	}
	if len(stale) != 0 {
		t.Fatalf("stale spelling still matches %#v", ids(stale))
	}
	bodyHit, err := s.Search("完全不同")
	if err != nil || len(bodyHit) != 1 {
		t.Fatalf("body search after rewrite = %#v err=%v", ids(bodyHit), err)
	}
	if err := s.MarkFailed(rewritten.ID, rewritten.Generation, "network down"); err != nil {
		t.Fatal(err)
	}
	failed, err := s.ListFailed()
	if err != nil {
		t.Fatal(err)
	}
	if len(failed) != 1 || failed[0].ID != item.ID || failed[0].Reason != "network down" || failed[0].FailedAt.IsZero() {
		t.Fatalf("failed = %#v", failed)
	}
	if err := s.Delete(item.ID); err != nil {
		t.Fatal(err)
	}
	failed, err = s.ListFailed()
	if err != nil {
		t.Fatal(err)
	}
	if len(failed) != 0 {
		t.Fatalf("failed after delete = %#v", failed)
	}
}

func TestLiteralAndMeaningSearch(t *testing.T) {
	s := openTestStore(t)
	both, err := s.Create("开头是垂直构图，结尾是海滩", "https://example.com/only-in-url")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create("只有海滩", ""); err != nil {
		t.Fatal(err)
	}
	alias, err := s.Create("喜欢剪纸风格", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CommitDerived(alias.ID, alias.Generation, derive.Result{
		Terms: []derive.Term{{
			Kind: "style", Phrase: "剪纸风格", Start: len("喜欢"), End: len("喜欢剪纸风格"), Spellings: []string{"paper-cut"},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	vertical, err := s.Search("垂直 海滩")
	if err != nil {
		t.Fatal(err)
	}
	if len(vertical) != 1 || vertical[0].ID != both.ID {
		t.Fatalf("vertical = %#v", ids(vertical))
	}
	spelled, err := s.Search("paper-cut")
	if err != nil {
		t.Fatal(err)
	}
	if len(spelled) != 1 || spelled[0].ID != alias.ID {
		t.Fatalf("spelling = %#v", ids(spelled))
	}
	fromURL, err := s.Search("only-in-url")
	if err != nil {
		t.Fatal(err)
	}
	if len(fromURL) != 0 {
		t.Fatalf("url matched %#v", fromURL)
	}

	high, err := s.Create("甲段", "")
	if err != nil {
		t.Fatal(err)
	}
	low, err := s.Create("乙段", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CommitDerived(high.ID, high.Generation, derive.Result{Vectors: []derive.Vector{{
		Source: "section", End: len("甲段"), Model: "m", Values: []float32{1, 0},
	}}}); err != nil {
		t.Fatal(err)
	}
	if err := s.CommitDerived(low.ID, low.Generation, derive.Result{Vectors: []derive.Vector{
		{Source: "section", End: len("乙段"), Model: "m", Values: []float32{0.6, 0.8}},
		{Source: "section", End: 1, Model: "other", Values: []float32{1, 0}},
	}}); err != nil {
		t.Fatal(err)
	}
	side, err := s.Create("丙段", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CommitDerived(side.ID, side.Generation, derive.Result{Vectors: []derive.Vector{{
		Source: "section", End: len("丙段"), Model: "m", Values: []float32{0, 1},
	}}}); err != nil {
		t.Fatal(err)
	}
	match, err := s.Match("番茄炒蛋", MatchConfig{
		Min: 0.5, Limit: 1,
		Embed: func(string) ([]float32, string, error) { return []float32{1, 0}, "m", nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(match.Literal) != 0 || len(match.Meaning) != 1 || match.Meaning[0].Prompt.ID != high.ID {
		t.Fatalf("meaning = literal %#v meaning %#v", idsOf(match.Literal), idsOf(match.Meaning))
	}
	literal, err := s.Match("海滩", MatchConfig{
		Min: 0, Limit: 5,
		Embed: func(string) ([]float32, string, error) { return []float32{1, 0}, "m", nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(literal.Literal) == 0 || len(literal.Meaning) != 0 {
		t.Fatalf("literal suppressed meaning: %#v %#v", idsOf(literal.Literal), idsOf(literal.Meaning))
	}
}

func idsOf(items []Hit) []string {
	out := make([]string, len(items))
	for i, item := range items {
		out[i] = item.Prompt.ID
	}
	return out
}

func ids(items []Prompt) []string {
	out := make([]string, len(items))
	for i, item := range items {
		out[i] = item.ID
	}
	return out
}
