package store

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"prompt-manager/internal/prompt"
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
	for _, name := range []string{"id", "body", "body_search", "example_url", "created_at", "updated_at"} {
		if !got[name] {
			t.Fatalf("missing column %s in %#v", name, got)
		}
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

func ids(items []Prompt) []string {
	out := make([]string, len(items))
	for i, item := range items {
		out[i] = item.ID
	}
	return out
}
