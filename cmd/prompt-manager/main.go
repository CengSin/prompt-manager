package main

import (
	"flag"
	"log"
	"net/http"
	"os"

	"prompt-manager/internal/derive"
	"prompt-manager/internal/store"
	"prompt-manager/internal/vector"
	"prompt-manager/internal/web"
)

func main() {
	migrateOnly := flag.Bool("migrate-vectors", false, "migrate existing vectors to Astra DB and exit")
	flag.Parse()
	addr, dbPath := settings(os.Getenv)
	st, err := store.Open(dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()
	cfg := derive.LoadFile(derive.ConfigPath)
	vectorCfg, err := vector.LoadFile(derive.ConfigPath)
	if err != nil {
		log.Fatal(err)
	}
	backend, err := vector.New(vectorCfg)
	if err != nil {
		log.Fatal(err)
	}
	if err := st.UseVectors(backend, vectorCfg.Ready()); err != nil {
		log.Fatal(err)
	}
	if !vectorCfg.Ready() {
		log.Print("Astra DB 未配置，语义搜索暂不可用；请填写 data/config.json 的 astra 配置")
	}
	if *migrateOnly {
		if !vectorCfg.Ready() {
			log.Fatal(vector.ErrNotConfigured)
		}
		log.Print("向量迁移完成")
		return
	}
	app := web.New(st, cfg)
	app.SetLimits(cfg.SimilarityMin, cfg.MeaningLimit)
	app.SetShortQueryMinimum(cfg.ShortQuerySimilarityMin)
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
