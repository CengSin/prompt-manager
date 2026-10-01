package store

import (
	"context"
	"database/sql"
	"time"

	"prompt-manager/internal/derive"
	"prompt-manager/internal/vector"
)

func (s *Store) UseVectors(backend vector.Backend, migrate bool) error {
	s.vectors = backend
	if !migrate {
		return nil
	}
	items, err := s.List()
	if err != nil {
		return err
	}
	for _, item := range items {
		err := s.withTx(func(tx *sql.Tx) error {
			rows, err := tx.Query(`SELECT source, start, end, model, vector FROM prompt_vectors WHERE prompt_id = ? AND generation = ? ORDER BY id`, item.ID, item.Generation)
			if err != nil {
				return err
			}
			var vectors []derive.Vector
			for rows.Next() {
				var v derive.Vector
				var blob []byte
				if err := rows.Scan(&v.Source, &v.Start, &v.End, &v.Model, &blob); err != nil {
					rows.Close()
					return err
				}
				v.Values = decodeVector(blob)
				vectors = append(vectors, v)
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return err
			}
			if len(vectors) == 0 {
				return nil
			}
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			if err := backend.Replace(ctx, vector.Revision{ID: item.ID, Generation: item.Generation}, vectors); err != nil {
				return err
			}
			_, err = tx.Exec(`DELETE FROM prompt_vectors WHERE prompt_id = ?`, item.ID)
			return err
		})
		if err != nil {
			return err
		}
	}
	return nil
}
