package history

import (
	"database/sql"
	"encoding/json"
	"fmt"

	_ "modernc.org/sqlite"
)

const defaultMaxSteps = 10000

type Store struct {
	db       *sql.DB
	maxSteps int
}

type Step struct {
	Position   int64  `json:"position"`
	EventJSON  string `json:"eventJson"`
	ResultJSON string `json:"resultJson"`
	EventType  string `json:"eventType"`
	StreamID   string `json:"streamId"`
	Status     string `json:"status"`
	Partition  string `json:"partition,omitempty"`
	HasEmit    bool   `json:"hasEmit"`
	HasLog     bool   `json:"hasLog"`
}

type TimelineEntry struct {
	Position  int64  `json:"position"`
	EventType string `json:"eventType,omitempty"`
	StreamID  string `json:"streamId,omitempty"`
	Status    string `json:"status"`
	Partition string `json:"partition,omitempty"`
	HasEmit   bool   `json:"hasEmit"`
	HasLog    bool   `json:"hasLog"`
}

func New() (*Store, error) {
	return NewWithLimit(defaultMaxSteps)
}

func NewWithLimit(maxSteps int) (*Store, error) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return nil, fmt.Errorf("history: open: %w", err)
	}

	if _, err := db.Exec(`
		CREATE TABLE steps (
			position    INTEGER PRIMARY KEY AUTOINCREMENT,
			event_json  TEXT NOT NULL,
			result_json TEXT NOT NULL,
			event_type  TEXT,
			stream_id   TEXT,
			status      TEXT,
			partition   TEXT,
			has_emit    BOOLEAN,
			has_log     BOOLEAN
		);
		CREATE INDEX idx_steps_status ON steps(status);
		CREATE INDEX idx_steps_stream ON steps(stream_id);
		CREATE INDEX idx_steps_type ON steps(event_type);
	`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("history: create tables: %w", err)
	}

	return &Store{db: db, maxSteps: maxSteps}, nil
}

func (s *Store) Insert(eventJSON, resultJSON string) (int64, error) {
	eventType, streamID := extractEventFields(eventJSON)
	status, partition, hasEmit, hasLog := extractResultFields(resultJSON)

	result, err := s.db.Exec(`
		INSERT INTO steps (event_json, result_json, event_type, stream_id, status, partition, has_emit, has_log)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, eventJSON, resultJSON, eventType, streamID, status, partition, hasEmit, hasLog)
	if err != nil {
		return 0, fmt.Errorf("history: insert: %w", err)
	}

	pos, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("history: last insert id: %w", err)
	}

	if int64(s.maxSteps) > 0 {
		_, _ = s.db.Exec(`DELETE FROM steps WHERE position <= ?`, pos-int64(s.maxSteps))
	}

	return pos, nil
}

func (s *Store) Get(position int64) (*Step, error) {
	row := s.db.QueryRow(`
		SELECT position, event_json, result_json, event_type, stream_id, status, partition, has_emit, has_log
		FROM steps WHERE position = ?
	`, position)
	return scanStep(row)
}

func (s *Store) Latest() (*Step, error) {
	row := s.db.QueryRow(`
		SELECT position, event_json, result_json, event_type, stream_id, status, partition, has_emit, has_log
		FROM steps ORDER BY position DESC LIMIT 1
	`)
	return scanStep(row)
}

func (s *Store) Timeline(from, to int64) ([]TimelineEntry, error) {
	return s.TimelineFiltered(from, to, "")
}

func (s *Store) TimelineFiltered(from, to int64, partition string) ([]TimelineEntry, error) {
	var query string
	var args []any

	if partition != "" {
		query = `SELECT position, event_type, stream_id, status, partition, has_emit, has_log
			FROM steps WHERE position >= ? AND position <= ? AND partition = ?
			ORDER BY position`
		args = []any{from, to, partition}
	} else {
		query = `SELECT position, event_type, stream_id, status, partition, has_emit, has_log
			FROM steps WHERE position >= ? AND position <= ?
			ORDER BY position`
		args = []any{from, to}
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("history: timeline: %w", err)
	}
	defer func() { _ = rows.Close() }()

	entries := []TimelineEntry{}
	for rows.Next() {
		var e TimelineEntry
		if err := rows.Scan(&e.Position, &e.EventType, &e.StreamID, &e.Status, &e.Partition, &e.HasEmit, &e.HasLog); err != nil {
			return nil, fmt.Errorf("history: scan timeline: %w", err)
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

func (s *Store) Count() (int64, error) {
	var count int64
	err := s.db.QueryRow(`SELECT COUNT(*) FROM steps`).Scan(&count)
	return count, err
}

func (s *Store) Range() (min, max int64, err error) {
	err = s.db.QueryRow(`SELECT COALESCE(MIN(position), 0), COALESCE(MAX(position), 0) FROM steps`).Scan(&min, &max)
	return
}

func (s *Store) Close() error {
	return s.db.Close()
}

func scanStep(row *sql.Row) (*Step, error) {
	var step Step
	err := row.Scan(
		&step.Position, &step.EventJSON, &step.ResultJSON,
		&step.EventType, &step.StreamID, &step.Status,
		&step.Partition, &step.HasEmit, &step.HasLog,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("history: scan: %w", err)
	}
	return &step, nil
}

func extractEventFields(eventJSON string) (eventType, streamID string) {
	var obj struct {
		EventType string `json:"eventType"`
		StreamID  string `json:"streamId"`
	}
	_ = json.Unmarshal([]byte(eventJSON), &obj)
	return obj.EventType, obj.StreamID
}

func extractResultFields(resultJSON string) (status, partition string, hasEmit, hasLog bool) {
	var obj struct {
		Status    string          `json:"status"`
		Partition string          `json:"partition"`
		Emitted   json.RawMessage `json:"emitted"`
		Logs      []string        `json:"logs"`
	}
	_ = json.Unmarshal([]byte(resultJSON), &obj)
	hasEmit = len(obj.Emitted) > 2 && string(obj.Emitted) != "null"
	hasLog = len(obj.Logs) > 0
	return obj.Status, obj.Partition, hasEmit, hasLog
}
