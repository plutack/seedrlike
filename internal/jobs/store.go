// Package jobs persists unfinished work and anonymous daily outcome counters.
package jobs

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

type Job struct {
	ID, InfoHash, Magnet, Name, Status, Stage, UserID string
	Zipped                                            bool
	Selected                                          []int
	Size, Reserved                                    int64
}

type Store struct{ db *sql.DB }

func New(db *sql.DB) *Store { return &Store{db: db} }
func (s *Store) Create(ctx context.Context, j *Job) error {
	selected, err := json.Marshal(j.Selected)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO Download_Jobs (ID,Info_Hash,Magnet,Name,Status,Stage,User_ID,Zipped,Selected_Files,Size,Reserved) VALUES (?,?,?,?,?,?,?,?,?,?,?)`, j.ID, j.InfoHash, j.Magnet, j.Name, j.Status, j.Stage, j.UserID, j.Zipped, selected, j.Size, j.Reserved)
	return err
}
func (s *Store) Active(ctx context.Context) ([]Job, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT ID,Info_Hash,Magnet,Name,Status,Stage,User_ID,Zipped,Selected_Files,Size,Reserved FROM Download_Jobs ORDER BY Created_At,ID`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Job{}
	for rows.Next() {
		var j Job
		var selected []byte
		if err = rows.Scan(&j.ID, &j.InfoHash, &j.Magnet, &j.Name, &j.Status, &j.Stage, &j.UserID, &j.Zipped, &selected, &j.Size, &j.Reserved); err != nil {
			return nil, err
		}
		if len(selected) > 0 {
			if err = json.Unmarshal(selected, &j.Selected); err != nil {
				return nil, fmt.Errorf("job %s selection: %w", j.ID, err)
			}
		}
		result = append(result, j)
	}
	return result, rows.Err()
}
func (s *Store) Save(ctx context.Context, j Job) error {
	selected, err := json.Marshal(j.Selected)
	if err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, `UPDATE Download_Jobs SET Name=?,Status=?,Stage=?,Selected_Files=?,Size=?,Reserved=? WHERE ID=?`, j.Name, j.Status, j.Stage, selected, j.Size, j.Reserved, j.ID)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 { // MySQL may report zero for an unchanged row.
		var exists int
		return s.db.QueryRowContext(ctx, `SELECT 1 FROM Download_Jobs WHERE ID=?`, j.ID).Scan(&exists)
	}
	return nil
}
func (s *Store) Finish(ctx context.Context, j Job, status string) error {
	if status != "completed" && status != "failed" && status != "stopped" {
		return fmt.Errorf("invalid terminal status")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `DELETE FROM Download_Jobs WHERE ID=?`, j.ID)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return tx.Commit()
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO Download_Metrics (Day,Status,Jobs,Bytes) VALUES (CURRENT_DATE(),?,1,?) ON DUPLICATE KEY UPDATE Jobs=Jobs+1, Bytes=Bytes+VALUES(Bytes)`, status, j.Size)
	if err != nil {
		return err
	}
	return tx.Commit()
}

type Metric struct {
	Status      string
	Jobs, Bytes int64
}

func (s *Store) Metrics(ctx context.Context) ([]Metric, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT Status,SUM(Jobs),SUM(Bytes) FROM Download_Metrics WHERE Day>=CURRENT_DATE()-INTERVAL 29 DAY GROUP BY Status`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Metric
	for rows.Next() {
		var m Metric
		if err = rows.Scan(&m.Status, &m.Jobs, &m.Bytes); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
