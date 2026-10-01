package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"prompt-manager/internal/derive"
	"prompt-manager/internal/vector"
)

type fakeVectors struct {
	replaced  []vector.Revision
	deleted   []string
	hits      []vector.Hit
	revisions []vector.Revision
	err       error
}

func (f *fakeVectors) Replace(_ context.Context, r vector.Revision, _ []derive.Vector) error {
	f.replaced = append(f.replaced, r)
	return f.err
}
func (f *fakeVectors) Delete(_ context.Context, id string) error {
	f.deleted = append(f.deleted, id)
	return f.err
}
func (f *fakeVectors) Search(_ context.Context, r []vector.Revision, _ []float32, _ string, _ float64, _ int) ([]vector.Hit, error) {
	f.revisions = r
	return f.hits, f.err
}

func TestRemoteVectorsMigrationAndLifecycle(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "prompts.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	p, err := st.Create("alpha", "")
	if err != nil {
		t.Fatal(err)
	}
	result := derive.Result{Vectors: []derive.Vector{{Source: "section", Start: 0, End: 5, Model: "m", Values: []float32{1, 0}}}}
	if err := st.CommitDerived(p.ID, p.Generation, result); err != nil {
		t.Fatal(err)
	}
	f := &fakeVectors{err: errors.New("offline")}
	if err := st.UseVectors(f, true); err == nil {
		t.Fatal("migration succeeded offline")
	}
	var n int
	st.db.QueryRow(`SELECT COUNT(*) FROM prompt_vectors`).Scan(&n)
	if n != 1 {
		t.Fatal("migration lost vectors")
	}
	f.err = nil
	if err := st.UseVectors(f, true); err != nil {
		t.Fatal(err)
	}
	st.db.QueryRow(`SELECT COUNT(*) FROM prompt_vectors`).Scan(&n)
	if n != 0 {
		t.Fatal("migration left local vectors")
	}
	if err := st.CommitDerived(p.ID, p.Generation, result); err != nil {
		t.Fatal(err)
	}
	st.db.QueryRow(`SELECT COUNT(*) FROM prompt_vectors`).Scan(&n)
	if n != 0 {
		t.Fatal("new vectors stored locally")
	}
	if _, err := st.Update(p.ID, p.Body, "https://example.com"); err != nil {
		t.Fatal(err)
	}
	if len(f.deleted) != 0 {
		t.Fatal("URL edit deleted vectors")
	}
	if _, err := st.Update(p.ID, "beta", ""); err != nil {
		t.Fatal(err)
	}
	if len(f.deleted) != 1 {
		t.Fatal("body edit did not delete vectors")
	}
	before := len(f.replaced)
	if err := st.CommitDerived(p.ID, 0, result); !errors.Is(err, ErrStale) {
		t.Fatal(err)
	}
	if len(f.replaced) != before {
		t.Fatal("stale job reached remote")
	}
	if err := st.Delete(p.ID); err != nil {
		t.Fatal(err)
	}
	if len(f.deleted) != 2 {
		t.Fatal("delete did not remove remote vectors")
	}
}

func TestRemoteMeaningKeepsLiteralAndCurrentGeneration(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "prompts.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	literal, _ := st.Create("alpha", "")
	semantic, _ := st.Create("beta", "")
	result := derive.Result{Vectors: []derive.Vector{{Values: []float32{1, 0}, Model: "m", Start: 0, End: 4}}}
	st.CommitDerived(literal.ID, 0, result)
	st.CommitDerived(semantic.ID, 0, result)
	f := &fakeVectors{hits: []vector.Hit{{PromptID: semantic.ID, Generation: 0, Start: 0, End: 4, Score: 0.8}}}
	if err := st.UseVectors(f, true); err != nil {
		t.Fatal(err)
	}
	cfg := MatchConfig{Min: 0.35, Limit: 5, Embed: func(string) ([]float32, string, error) { return []float32{1, 0}, "m", nil }}
	match, err := st.Match("alpha", cfg)
	if err != nil || len(match.Literal) != 1 || len(match.Meaning) != 1 || match.Meaning[0].Prompt.ID != semantic.ID || len(f.revisions) != 1 || f.revisions[0].ID != semantic.ID {
		t.Fatalf("%#v %v candidates=%#v", match, err, f.revisions)
	}
	f.err = errors.New("offline")
	match, err = st.Match("alpha", cfg)
	if err != nil || len(match.Literal) != 1 || len(match.Meaning) != 0 {
		t.Fatal(match, err)
	}
}
