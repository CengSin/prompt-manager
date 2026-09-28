package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"prompt-manager/internal/store"
	"prompt-manager/internal/web"
)

func TestSettings(t *testing.T) {
	addr, dbPath := settings(func(key string) string {
		switch key {
		case "PORT":
			return "9090"
		case "PROMPT_MANAGER_DB":
			return "/tmp/prompts.db"
		default:
			return ""
		}
	})
	if addr != "127.0.0.1:9090" || dbPath != "/tmp/prompts.db" {
		t.Fatalf("custom settings = %s %s", addr, dbPath)
	}
	addr, dbPath = settings(func(string) string { return "" })
	if addr != "127.0.0.1:8787" || dbPath != "./data/prompts.db" {
		t.Fatalf("default settings = %s %s", addr, dbPath)
	}
}

func TestEmptyLibraryPage(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "prompts.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ts := httptest.NewServer(web.NewHandler(st))
	t.Cleanup(ts.Close)

	res, err := ts.Client().Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK || !strings.Contains(string(body), "还没有提示词") {
		t.Fatalf("status=%d body=%s", res.StatusCode, body)
	}
}
