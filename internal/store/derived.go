package store

import (
	"database/sql"
	"encoding/binary"
	"errors"
	"math"
	"strings"
	"time"

	"prompt-manager/internal/derive"
	"prompt-manager/internal/prompt"
	"prompt-manager/internal/search"
)

var ErrStale = errors.New("stale derivation")

const (
	StatusNone    = "none"
	StatusPending = "pending"
	StatusReady   = "ready"
	StatusFailed  = "failed"
)

type Failure struct {
	Prompt
	Reason   string
	FailedAt time.Time
}

type Hit struct {
	Prompt Prompt
	Spans  []prompt.Span
	Score  float64
}

type Match struct {
	Literal []Hit
	Meaning []Hit
}

type EmbedFunc func(text string) ([]float32, string, error)

type MatchConfig struct {
	Min   float64
	Limit int
	Embed EmbedFunc
}

func migrate(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS prompts (
		id TEXT PRIMARY KEY,
		body TEXT NOT NULL,
		body_search TEXT NOT NULL,
		example_url TEXT,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		derive_generation INTEGER NOT NULL DEFAULT 0,
		derive_status TEXT NOT NULL DEFAULT 'none',
		derive_error TEXT,
		derive_failed_at TEXT
	)`); err != nil {
		return err
	}
	columns := []struct{ name, decl string }{
		{"derive_generation", "INTEGER NOT NULL DEFAULT 0"},
		{"derive_status", "TEXT NOT NULL DEFAULT 'none'"},
		{"derive_error", "TEXT"},
		{"derive_failed_at", "TEXT"},
	}
	for _, column := range columns {
		ok, err := columnExists(db, "prompts", column.name)
		if err != nil {
			return err
		}
		if ok {
			continue
		}
		if _, err := db.Exec(`ALTER TABLE prompts ADD COLUMN ` + column.name + ` ` + column.decl); err != nil {
			return err
		}
	}
	for _, statement := range []string{
		`CREATE TABLE IF NOT EXISTS prompt_terms (
			id TEXT PRIMARY KEY,
			prompt_id TEXT NOT NULL,
			kind TEXT NOT NULL,
			phrase TEXT NOT NULL,
			start INTEGER NOT NULL,
			end INTEGER NOT NULL,
			generation INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS prompt_spellings (
			term_id TEXT NOT NULL,
			spelling TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS prompt_vectors (
			id TEXT PRIMARY KEY,
			prompt_id TEXT NOT NULL,
			source TEXT NOT NULL,
			source_id TEXT,
			start INTEGER NOT NULL,
			end INTEGER NOT NULL,
			model TEXT NOT NULL,
			dim INTEGER NOT NULL,
			vector BLOB NOT NULL,
			generation INTEGER NOT NULL
		)`,
	} {
		if _, err := db.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}

func columnExists(db *sql.DB, table, column string) (bool, error) {
	rows, err := db.Query(`SELECT name FROM pragma_table_info('` + table + `')`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

func (s *Store) CommitDerived(id string, generation int, result derive.Result) error {
	return s.withTx(func(tx *sql.Tx) error {
		current, err := generationOf(tx, id)
		if err != nil {
			return err
		}
		if current != generation {
			return ErrStale
		}
		if err := deleteDerived(tx, id); err != nil {
			return err
		}
		for _, term := range result.Terms {
			termID, err := newID()
			if err != nil {
				return err
			}
			if _, err := tx.Exec(
				`INSERT INTO prompt_terms (id, prompt_id, kind, phrase, start, end, generation) VALUES (?, ?, ?, ?, ?, ?, ?)`,
				termID, id, term.Kind, term.Phrase, term.Start, term.End, generation,
			); err != nil {
				return err
			}
			for _, spelling := range term.Spellings {
				if _, err := tx.Exec(`INSERT INTO prompt_spellings (term_id, spelling) VALUES (?, ?)`, termID, spelling); err != nil {
					return err
				}
			}
		}
		for _, vec := range result.Vectors {
			vecID, err := newID()
			if err != nil {
				return err
			}
			if _, err := tx.Exec(
				`INSERT INTO prompt_vectors (id, prompt_id, source, source_id, start, end, model, dim, vector, generation) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				vecID, id, vec.Source, "", vec.Start, vec.End, vec.Model, len(vec.Values), encodeVector(vec.Values), generation,
			); err != nil {
				return err
			}
		}
		_, err = tx.Exec(
			`UPDATE prompts SET derive_status = ?, derive_error = NULL, derive_failed_at = NULL WHERE id = ? AND derive_generation = ?`,
			StatusReady, id, generation,
		)
		return err
	})
}

func (s *Store) MarkPending(id string, generation int) error {
	return s.setStatus(id, generation, StatusPending, "", nil)
}

func (s *Store) MarkFailed(id string, generation int, reason string) error {
	now := formatTime(s.now())
	return s.setStatus(id, generation, StatusFailed, reason, &now)
}

func (s *Store) MarkIdle(id string, generation int) error {
	return s.setStatus(id, generation, StatusNone, "", nil)
}

func (s *Store) setStatus(id string, generation int, status, reason string, failedAt *string) error {
	return s.withTx(func(tx *sql.Tx) error {
		current, err := generationOf(tx, id)
		if err != nil {
			return err
		}
		if current != generation {
			return ErrStale
		}
		var failed any
		if failedAt != nil {
			failed = *failedAt
		}
		var storedReason any
		if reason != "" {
			storedReason = reason
		}
		_, err = tx.Exec(
			`UPDATE prompts SET derive_status = ?, derive_error = ?, derive_failed_at = ? WHERE id = ? AND derive_generation = ?`,
			status, storedReason, failed, id, generation,
		)
		return err
	})
}

func (s *Store) ListFailed() ([]Failure, error) {
	rows, err := s.db.Query(
		`SELECT id, body, example_url, created_at, updated_at, derive_generation, derive_status, derive_error, derive_failed_at
		 FROM prompts WHERE derive_status = ? ORDER BY derive_failed_at DESC, id DESC`,
		StatusFailed,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Failure
	for rows.Next() {
		var item Failure
		var example sql.NullString
		var created, updated string
		var reason, failed sql.NullString
		if err := rows.Scan(&item.ID, &item.Body, &example, &created, &updated, &item.Generation, &item.Status, &reason, &failed); err != nil {
			return nil, err
		}
		if example.Valid {
			item.ExampleURL = example.String
		}
		if reason.Valid {
			item.Reason = reason.String
		}
		item.CreatedAt, err = time.Parse(timeLayout, created)
		if err != nil {
			return nil, err
		}
		item.UpdatedAt, err = time.Parse(timeLayout, updated)
		if err != nil {
			return nil, err
		}
		if failed.Valid {
			item.FailedAt, err = time.Parse(timeLayout, failed.String)
			if err != nil {
				return nil, err
			}
		}
		out = append(out, item)
	}
	if out == nil {
		out = []Failure{}
	}
	return out, rows.Err()
}

func (s *Store) CountFailed() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM prompts WHERE derive_status = ?`, StatusFailed).Scan(&n)
	return n, err
}

func (s *Store) CountPending() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM prompts WHERE derive_status = ?`, StatusPending).Scan(&n)
	return n, err
}

func (s *Store) ListUnprocessed() ([]Prompt, error) {
	return s.query(`WHERE derive_status IN (?, ?)`, StatusNone, StatusPending)
}

func (s *Store) Match(query string, cfg MatchConfig) (Match, error) {
	docs, prompts, err := s.loadDocs()
	if err != nil {
		return Match{}, err
	}
	if strings.TrimSpace(query) == "" {
		hits := make([]Hit, len(prompts))
		for i, item := range prompts {
			hits[i] = Hit{Prompt: item}
		}
		return Match{Literal: hits}, nil
	}
	literal := search.Literal(docs, query)
	match := Match{Literal: attach(prompts, literal)}
	if cfg.Embed == nil || cfg.Limit <= 0 {
		return match, nil
	}
	vec, model, err := cfg.Embed(query)
	if err != nil || len(vec) == 0 {
		return match, nil
	}
	literalIDs := make(map[string]struct{}, len(match.Literal))
	for _, hit := range match.Literal {
		literalIDs[hit.Prompt.ID] = struct{}{}
	}
	for _, hit := range attach(prompts, search.Meaning(docs, vec, model, cfg.Min, len(docs))) {
		if _, exists := literalIDs[hit.Prompt.ID]; exists {
			continue
		}
		match.Meaning = append(match.Meaning, hit)
		if len(match.Meaning) == cfg.Limit {
			break
		}
	}
	return match, nil
}

func attach(prompts []Prompt, hits []search.Hit) []Hit {
	out := make([]Hit, 0, len(hits))
	for _, hit := range hits {
		if hit.Index < 0 || hit.Index >= len(prompts) {
			continue
		}
		out = append(out, Hit{Prompt: prompts[hit.Index], Spans: hit.Spans, Score: hit.Score})
	}
	return out
}

func (s *Store) loadDocs() ([]search.Doc, []Prompt, error) {
	prompts, err := s.List()
	if err != nil {
		return nil, nil, err
	}
	gen := map[string]int{}
	index := map[string]int{}
	docs := make([]search.Doc, len(prompts))
	for i, item := range prompts {
		gen[item.ID] = item.Generation
		index[item.ID] = i
		docs[i] = search.Doc{ID: item.ID, Body: item.Body}
	}
	rows, err := s.db.Query(`SELECT id, prompt_id, kind, phrase, start, end, generation FROM prompt_terms`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	termAt := map[string]struct {
		doc   int
		index int
	}{}
	for rows.Next() {
		var id, promptID, kind, phrase string
		var start, end, generation int
		if err := rows.Scan(&id, &promptID, &kind, &phrase, &start, &end, &generation); err != nil {
			return nil, nil, err
		}
		docIndex, ok := index[promptID]
		if !ok || gen[promptID] != generation {
			continue
		}
		docs[docIndex].Terms = append(docs[docIndex].Terms, search.Term{
			Kind: kind, Phrase: phrase, Start: start, End: end,
		})
		termAt[id] = struct {
			doc   int
			index int
		}{doc: docIndex, index: len(docs[docIndex].Terms) - 1}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	spellingRows, err := s.db.Query(`SELECT term_id, spelling FROM prompt_spellings`)
	if err != nil {
		return nil, nil, err
	}
	defer spellingRows.Close()
	for spellingRows.Next() {
		var termID, spelling string
		if err := spellingRows.Scan(&termID, &spelling); err != nil {
			return nil, nil, err
		}
		at, ok := termAt[termID]
		if !ok {
			continue
		}
		docs[at.doc].Terms[at.index].Spellings = append(docs[at.doc].Terms[at.index].Spellings, spelling)
	}
	if err := spellingRows.Err(); err != nil {
		return nil, nil, err
	}
	vecRows, err := s.db.Query(`SELECT prompt_id, start, end, model, vector, generation FROM prompt_vectors`)
	if err != nil {
		return nil, nil, err
	}
	defer vecRows.Close()
	for vecRows.Next() {
		var promptID, model string
		var start, end, generation int
		var blob []byte
		if err := vecRows.Scan(&promptID, &start, &end, &model, &blob, &generation); err != nil {
			return nil, nil, err
		}
		docIndex, ok := index[promptID]
		if !ok || gen[promptID] != generation {
			continue
		}
		docs[docIndex].Vecs = append(docs[docIndex].Vecs, search.Vector{
			Start: start, End: end, Model: model, Values: decodeVector(blob),
		})
	}
	return docs, prompts, vecRows.Err()
}

func (s *Store) withTx(fn func(*sql.Tx) error) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

type queryExec interface {
	Exec(query string, args ...any) (sql.Result, error)
	QueryRow(query string, args ...any) *sql.Row
}

func generationOf(q queryExec, id string) (int, error) {
	var gen int
	err := q.QueryRow(`SELECT derive_generation FROM prompts WHERE id = ?`, id).Scan(&gen)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	return gen, err
}

func deleteDerived(tx *sql.Tx, id string) error {
	if _, err := tx.Exec(`DELETE FROM prompt_spellings WHERE term_id IN (SELECT id FROM prompt_terms WHERE prompt_id = ?)`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM prompt_terms WHERE prompt_id = ?`, id); err != nil {
		return err
	}
	_, err := tx.Exec(`DELETE FROM prompt_vectors WHERE prompt_id = ?`, id)
	return err
}

func encodeVector(values []float32) []byte {
	buf := make([]byte, 4*len(values))
	for i, value := range values {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(value))
	}
	return buf
}

func decodeVector(buf []byte) []float32 {
	if len(buf)%4 != 0 {
		return nil
	}
	out := make([]float32, len(buf)/4)
	for i := range out {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(buf[i*4:]))
	}
	return out
}
