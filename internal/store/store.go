package store

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"prompt-manager/internal/prompt"

	"crypto/rand"
	_ "modernc.org/sqlite"
)

const timeLayout = "2006-01-02T15:04:05.000000000Z"

var ErrNotFound = errors.New("prompt not found")

type Prompt struct {
	ID         string
	Body       string
	ExampleURL string
	CreatedAt  time.Time
	UpdatedAt  time.Time
	Generation int
	Status     string
}

type Store struct {
	db  *sql.DB
	now func() time.Time
}

func Open(path string) (*Store, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{
		db:  db,
		now: func() time.Time { return time.Now().UTC() },
	}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) Create(body, exampleURL string) (Prompt, error) {
	draft, err := prompt.Validate(body, exampleURL)
	if err != nil {
		return Prompt{}, err
	}
	id, err := newID()
	if err != nil {
		return Prompt{}, err
	}
	now := formatTime(s.now())
	_, err = s.db.Exec(
		`INSERT INTO prompts (id, body, body_search, example_url, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		id, draft.Body, draft.BodySearch, nullableURL(draft), now, now,
	)
	if err != nil {
		return Prompt{}, err
	}
	return s.Get(id)
}

func (s *Store) Get(id string) (Prompt, error) {
	row := s.db.QueryRow(
		`SELECT id, body, example_url, created_at, updated_at, derive_generation, derive_status FROM prompts WHERE id = ?`,
		id,
	)
	return scanPrompt(row)
}

func (s *Store) List() ([]Prompt, error) {
	return s.query(``)
}

func (s *Store) Search(query string) ([]Prompt, error) {
	if strings.TrimSpace(query) == "" {
		return s.List()
	}
	match, err := s.Match(query, MatchConfig{})
	if err != nil {
		return nil, err
	}
	out := make([]Prompt, len(match.Literal))
	for i, hit := range match.Literal {
		out[i] = hit.Prompt
	}
	return out, nil
}

func (s *Store) Update(id, body, exampleURL string) (Prompt, error) {
	current, err := s.Get(id)
	if err != nil {
		return Prompt{}, err
	}
	draft, err := prompt.Validate(body, exampleURL)
	if err != nil {
		return Prompt{}, err
	}
	if current.Body == draft.Body {
		_, err = s.db.Exec(
			`UPDATE prompts SET example_url = ?, updated_at = ? WHERE id = ?`,
			nullableURL(draft), formatTime(s.now()), id,
		)
		if err != nil {
			return Prompt{}, err
		}
		return s.Get(id)
	}
	err = s.withTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(
			`UPDATE prompts SET body = ?, body_search = ?, example_url = ?, updated_at = ?, derive_generation = ?, derive_status = 'none', derive_error = NULL, derive_failed_at = NULL WHERE id = ?`,
			draft.Body, draft.BodySearch, nullableURL(draft), formatTime(s.now()), current.Generation+1, id,
		); err != nil {
			return err
		}
		return deleteDerived(tx, id)
	})
	if err != nil {
		return Prompt{}, err
	}
	return s.Get(id)
}

func (s *Store) Delete(id string) error {
	return s.withTx(func(tx *sql.Tx) error {
		if err := deleteDerived(tx, id); err != nil {
			return err
		}
		result, err := tx.Exec(`DELETE FROM prompts WHERE id = ?`, id)
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			return ErrNotFound
		}
		return nil
	})
}

func (s *Store) query(where string, args ...any) ([]Prompt, error) {
	rows, err := s.db.Query(
		`SELECT id, body, example_url, created_at, updated_at, derive_generation, derive_status FROM prompts `+where+` ORDER BY created_at DESC, id DESC`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Prompt
	for rows.Next() {
		item, err := scanPrompt(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	if out == nil {
		out = []Prompt{}
	}
	return out, rows.Err()
}

type scanner interface {
	Scan(dest ...any) error
}

func scanPrompt(row scanner) (Prompt, error) {
	var item Prompt
	var example sql.NullString
	var created, updated string
	if err := row.Scan(&item.ID, &item.Body, &example, &created, &updated, &item.Generation, &item.Status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Prompt{}, ErrNotFound
		}
		return Prompt{}, err
	}
	if example.Valid {
		item.ExampleURL = example.String
	}
	var err error
	item.CreatedAt, err = time.Parse(timeLayout, created)
	if err != nil {
		return Prompt{}, err
	}
	item.UpdatedAt, err = time.Parse(timeLayout, updated)
	if err != nil {
		return Prompt{}, err
	}
	return item, nil
}

func nullableURL(draft prompt.Draft) any {
	if !draft.HasExample {
		return nil
	}
	return draft.ExampleURL
}

func formatTime(t time.Time) string {
	return t.UTC().Format(timeLayout)
}

func newID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:]), nil
}
