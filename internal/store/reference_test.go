package store

import (
	"errors"
	"math"
	"path/filepath"
	"testing"

	"prompt-manager/internal/derive"
	"prompt-manager/internal/vector"
)

func TestReferencesSelectsWritingExamples(t *testing.T) {
	s := openTestStore(t)
	calls := 0
	embed := func(text string) ([]float32, string, error) {
		calls++
		if text == "" {
			t.Fatal("embedded an empty need")
		}
		return []float32{1, 0}, "m", nil
	}
	add := func(body, example string, values []float32) Prompt {
		t.Helper()
		item, err := s.Create(body, example)
		if err != nil {
			t.Fatal(err)
		}
		if values == nil {
			return item
		}
		if err := s.CommitDerived(item.ID, item.Generation, derive.Result{Vectors: []derive.Vector{{
			Source: "section", End: len(body), Model: "m", Values: values,
		}}}); err != nil {
			t.Fatal(err)
		}
		return item
	}
	unit := func(x float64) []float32 {
		return []float32{float32(x), float32(math.Sqrt(1 - x*x))}
	}
	first := add("第一全文", "", unit(1))
	second := add("第二全文", "", unit(0.8))
	third := add("第三全文", "", unit(0.6))
	below := add("第四全文", "", unit(0.2))
	got, err := s.References("无关的需求", 0.5, embed)
	if err != nil {
		t.Fatal(err)
	}
	if ids(got)[0] != first.ID || ids(got)[1] != second.ID || ids(got)[2] != third.ID || len(got) != 3 {
		t.Fatalf("top three = %v", ids(got))
	}
	for _, item := range got {
		if item.ID == below.ID {
			t.Fatal("below-minimum prompt was selected")
		}
	}

	fresh := openTestStore(t)
	addOn := func(body string, x float64) Prompt {
		t.Helper()
		item, err := fresh.Create(body, "")
		if err != nil {
			t.Fatal(err)
		}
		if err := fresh.CommitDerived(item.ID, item.Generation, derive.Result{Vectors: []derive.Vector{{
			Source: "section", End: len(body), Model: "m", Values: unit(x),
		}}}); err != nil {
			t.Fatal(err)
		}
		return item
	}
	highest := addOn("甲", 1)
	addOn("乙", 0.9)
	addOn("丙", 0.8)
	addOn("丁", 0.7)
	addOn("戊", 0.6)
	capped, err := fresh.References("再写一条", 0.5, embed)
	if err != nil {
		t.Fatal(err)
	}
	if len(capped) != 3 || capped[0].ID != highest.ID {
		t.Fatalf("cap = %v", ids(capped))
	}

	literal := add("热带海滩", "https://example.com/do-not-send", unit(1))
	got, err = s.References("海滩", 0.35, embed)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 || got[0].ID != literal.ID || got[0].Body != "热带海滩" || got[0].ExampleURL != "https://example.com/do-not-send" {
		t.Fatalf("literal example = %#v", got)
	}

	shortStore := openTestStore(t)
	shortItem, err := shortStore.Create("短需求范例", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := shortStore.CommitDerived(shortItem.ID, shortItem.Generation, derive.Result{Vectors: []derive.Vector{{
		Source: "section", End: len(shortItem.Body), Model: "m", Values: unit(0.4),
	}}}); err != nil {
		t.Fatal(err)
	}
	got, err = shortStore.References("剪纸", 0.35, embed)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != shortItem.ID {
		t.Fatal("ordinary minimum excluded a short need")
	}

	bare := add("没有向量", "", nil)
	got, err = s.References("没有向量", 0.35, embed)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range got {
		if item.ID == bare.ID {
			t.Fatal("prompt without vectors was selected")
		}
	}

	failed := 0
	got, err = s.References("海滩", 0.35, func(string) ([]float32, string, error) {
		failed++
		return nil, "", errors.New("embedding down")
	})
	if err != nil || len(got) != 0 || failed != 1 {
		t.Fatalf("embed failure got=%v err=%v calls=%d", ids(got), err, failed)
	}
	got, err = s.References("海滩", 0.35, nil)
	if err != nil || len(got) != 0 {
		t.Fatalf("nil embed got=%v err=%v", ids(got), err)
	}
	if calls == 0 {
		t.Fatal("successful retrieval did not embed")
	}
}

func TestReferencesIncludeLiteralOnRemoteVectors(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "prompts.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	literal, err := st.Create("alpha", "")
	if err != nil {
		t.Fatal(err)
	}
	other, err := st.Create("beta", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Create("gamma", ""); err != nil {
		t.Fatal(err)
	}
	result := derive.Result{Vectors: []derive.Vector{{Source: "section", Start: 0, End: 5, Model: "m", Values: []float32{1, 0}}}}
	if err := st.CommitDerived(literal.ID, literal.Generation, result); err != nil {
		t.Fatal(err)
	}
	result.Vectors[0].End = 4
	if err := st.CommitDerived(other.ID, other.Generation, result); err != nil {
		t.Fatal(err)
	}
	f := &fakeVectors{hits: []vector.Hit{
		{PromptID: literal.ID, Generation: literal.Generation, Start: 0, End: len(literal.Body), Score: 0.9},
		{PromptID: other.ID, Generation: other.Generation, Start: 0, End: len(other.Body), Score: 0.5},
	}}
	if err := st.UseVectors(f, true); err != nil {
		t.Fatal(err)
	}
	got, err := st.References("alpha", 0.35, func(string) ([]float32, string, error) {
		return []float32{1, 0}, "m", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != literal.ID || got[1].ID != other.ID {
		t.Fatalf("remote examples = %v", ids(got))
	}
	if len(f.revisions) != 2 {
		t.Fatalf("candidates = %#v", f.revisions)
	}
	seen := map[string]bool{}
	for _, revision := range f.revisions {
		seen[revision.ID] = true
	}
	if !seen[literal.ID] || !seen[other.ID] {
		t.Fatalf("literal candidate missing: %#v", f.revisions)
	}
}
