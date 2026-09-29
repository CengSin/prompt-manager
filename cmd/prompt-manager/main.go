package main

import (
	"log"
	"net/http"
	"os"

	"prompt-manager/internal/derive"
	"prompt-manager/internal/store"
	"prompt-manager/internal/web"
)

func main() {
	addr, dbPath := settings(os.Getenv)
	st, err := store.Open(dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()
	cfg := derive.LoadFile(derive.ConfigPath)
	app := web.New(st, cfg)
	app.SetLimits(cfg.SimilarityMin, cfg.MeaningLimit)
	app.Start()
	defer app.Stop()
	app.Backfill()
	log.Printf("提示词库 http://%s", addr)
	if err := http.ListenAndServe(addr, app); err != nil {
		log.Fatal(err)
	}
}

func settings(getenv func(string) string) (string, string) {
	dbPath := getenv("PROMPT_MANAGER_DB")
	if dbPath == "" {
		dbPath = "./data/prompts.db"
	}
	return web.ListenAddr(getenv("PORT")), dbPath
}
