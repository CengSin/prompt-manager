package vector

import (
	"context"
	"database/sql"
	"encoding/binary"
	"math"
	"os"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestConfiguredAstraQuery(t *testing.T) {
	if os.Getenv("RUN_ASTRA_INTEGRATION") != "1" {
		t.Skip("requires configured Astra DB and pre-migration backup")
	}
	cfg, err := LoadFile("../../data/config.json")
	if err != nil {
		t.Fatal(err)
	}
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", "file:../../data/prompts-before-astra.db?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var rev Revision
	var model string
	var blob []byte
	if err := db.QueryRow(`SELECT prompt_id,generation,model,vector FROM prompt_vectors LIMIT 1`).Scan(&rev.ID, &rev.Generation, &model, &blob); err != nil {
		t.Fatal(err)
	}
	values := make([]float32, len(blob)/4)
	for i := range values {
		values[i] = math.Float32frombits(binary.LittleEndian.Uint32(blob[i*4:]))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	hits, err := a.Search(ctx, []Revision{rev}, values, model, 0.99, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].PromptID != rev.ID || hits[0].Score < 0.99 {
		t.Fatalf("Astra exact-vector query failed: %#v", hits)
	}
	t.Logf("Astra query passed: dimension=%d cosine=%.6f", len(values), hits[0].Score)
}
