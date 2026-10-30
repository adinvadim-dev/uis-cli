// Package archive is the local SQLite implementation of archive-contract.md.
// crawlkit adoption is deferred until its toolchain fits the supported Go version.
package archive

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"uis-cli/internal/portal"
)

type Store struct{ db *sql.DB }

func Open(dir string) (*Store, error) {
	if e := os.MkdirAll(dir, 0700); e != nil {
		return nil, e
	}
	path := filepath.Join(dir, "support.sqlite")
	f, e := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	f.Close()
	if e = os.Chmod(path, 0600); e != nil {
		return nil, e
	}
	db, e := sql.Open("sqlite3", path+"?_foreign_keys=on&_busy_timeout=5000")
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(1)
	_, e = db.Exec(`CREATE TABLE IF NOT EXISTS tickets(id TEXT PRIMARY KEY,payload TEXT NOT NULL,observed_at TEXT NOT NULL);CREATE TABLE IF NOT EXISTS comments(id TEXT PRIMARY KEY,ticket_id TEXT NOT NULL REFERENCES tickets(id),payload TEXT NOT NULL,observed_at TEXT NOT NULL);CREATE TABLE IF NOT EXISTS state(key TEXT PRIMARY KEY,value TEXT NOT NULL);CREATE VIRTUAL TABLE IF NOT EXISTS search USING fts4(ticket_id,body);`)
	if e != nil {
		db.Close()
		return nil, e
	}
	return &Store{db}, nil
}
func (s *Store) Close() error { return s.db.Close() }

// Scrub removes session material and bearer/download locators before persistence.
func Scrub(v interface{}) interface{} {
	switch x := v.(type) {
	case map[string]interface{}:
		o := map[string]interface{}{}
		for k, v := range x {
			lower := strings.ToLower(k)
			if strings.Contains(lower, "token") || strings.Contains(lower, "cookie") || strings.Contains(lower, "csrf") || lower == "url" || strings.HasSuffix(lower, "_url") || strings.Contains(lower, "record_file") || lower == "call_records" || lower == "wav_call_records" || lower == "voice_mail_records" || lower == "talk_file" || lower == "voice_file" || strings.Contains(lower, "password") {
				continue
			}
			o[k] = Scrub(v)
		}
		if _, ok := x["filename"]; ok {
			o["preservation_status"] = "pending"
		}
		return o
	case []portal.Object:
		a := make([]interface{}, len(x))
		for i, v := range x {
			a[i] = Scrub(v)
		}
		return a
	case []interface{}:
		a := make([]interface{}, len(x))
		for i, v := range x {
			a[i] = Scrub(v)
		}
		return a
	default:
		return v
	}
}
func (s *Store) Page(collection, ticketID string, rows []portal.Object, cursor string) error {
	tx, e := s.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	for _, r := range rows {
		id := portal.ID(r)
		if id == "" {
			return errors.New("missing provider identity")
		}
		b, e := json.Marshal(Scrub(r))
		if e != nil {
			return e
		}
		if collection == "tickets" {
			_, e = tx.Exec(`INSERT INTO tickets VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET payload=excluded.payload,observed_at=excluded.observed_at`, id, string(b), time.Now().UTC().Format(time.RFC3339))
		} else {
			_, e = tx.Exec(`INSERT INTO comments VALUES(?,?,?,?) ON CONFLICT(id) DO UPDATE SET payload=excluded.payload,observed_at=excluded.observed_at`, id, ticketID, string(b), time.Now().UTC().Format(time.RFC3339))
		}
		if e != nil {
			return e
		}
	}
	_, e = tx.Exec(`INSERT INTO state VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, "cursor:"+collection+":"+ticketID, cursor)
	if e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Store) Set(k, v string) error {
	_, e := s.db.Exec(`INSERT INTO state VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, k, v)
	return e
}
func (s *Store) Status() (map[string]interface{}, error) {
	rows, e := s.db.Query(`SELECT key,value FROM state`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	m := map[string]interface{}{"schemaVersion": "crawlkit.control.v1", "backend": "local-sqlite", "ready": false, "last_successful_sync": nil, "degraded": false, "error_count": 0, "attachment_bytes": "not_preserved"}
	checkpoints := map[string]string{}
	for rows.Next() {
		var k, v string
		if e = rows.Scan(&k, &v); e != nil {
			return nil, e
		}
		if strings.HasPrefix(k, "cursor:") {
			checkpoints[k] = v
		} else if k == "degraded" {
			m[k] = v != "" && v != "false"
			if v != "" && v != "false" {
				m["error_count"] = 1
			}
		} else {
			m[k] = v
			if k == "last_successful_sync" {
				m["ready"] = true
			}
		}
	}
	m["checkpoints"] = checkpoints
	return m, rows.Err()
}
func decode(b string) (portal.Object, error) {
	var o portal.Object
	d := json.NewDecoder(strings.NewReader(b))
	d.UseNumber()
	e := d.Decode(&o)
	return o, e
}
func (s *Store) List(status, query string, limit, page int) ([]portal.Object, error) {
	rows, e := s.db.Query(`SELECT payload FROM tickets ORDER BY id`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []portal.Object{}
	skip := (page - 1) * limit
	for rows.Next() {
		var b string
		if e = rows.Scan(&b); e != nil {
			return nil, e
		}
		o, e := decode(b)
		if e != nil {
			return nil, e
		}
		if status != "" && fmt.Sprint(o["status"]) != status {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(b), strings.ToLower(query)) {
			continue
		}
		if skip > 0 {
			skip--
			continue
		}
		if len(out) < limit {
			out = append(out, o)
		}
	}
	return out, rows.Err()
}
func (s *Store) Show(id string) (portal.Object, error) {
	var b string
	if e := s.db.QueryRow(`SELECT payload FROM tickets WHERE id=?`, id).Scan(&b); e != nil {
		return nil, e
	}
	t, e := decode(b)
	if e != nil {
		return nil, e
	}
	rows, e := s.db.Query(`SELECT payload FROM comments WHERE ticket_id=? ORDER BY json_extract(payload,'$.date_time'),id`, id)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	comments := []portal.Object{}
	for rows.Next() {
		if e = rows.Scan(&b); e != nil {
			return nil, e
		}
		o, e := decode(b)
		if e != nil {
			return nil, e
		}
		comments = append(comments, o)
	}
	return portal.Object{"ticket": t, "comments": comments}, rows.Err()
}
func (s *Store) Search(query, status string, limit, offset int) ([]portal.Object, error) {
	rows, e := s.db.Query(`SELECT DISTINCT tickets.payload FROM search JOIN tickets ON tickets.id=search.ticket_id WHERE search MATCH ? AND (?='' OR json_extract(tickets.payload,'$.status')=?) ORDER BY tickets.id LIMIT ? OFFSET ?`, query, status, status, limit, offset)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []portal.Object{}
	for rows.Next() {
		var b string
		if e = rows.Scan(&b); e != nil {
			return nil, e
		}
		o, e := decode(b)
		if e != nil {
			return nil, e
		}
		out = append(out, o)
	}
	return out, rows.Err()
}
func (s *Store) index() error {
	tx, e := s.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.Exec(`DELETE FROM search`); e != nil {
		return e
	}
	if _, e = tx.Exec(`INSERT INTO search SELECT id,payload FROM tickets UNION ALL SELECT ticket_id,payload FROM comments`); e != nil {
		return e
	}
	return tx.Commit()
}

type Source interface {
	Tickets(context.Context, int, int) ([]portal.Object, error)
	Comments(context.Context, portal.Object, int, int) ([]portal.Object, error)
}

// Every run rediscovers all tickets and threads: more work, but catches replies on
// old tickets without relying on undocumented update ordering or cursor semantics.
func (s *Store) Sync(ctx context.Context, src Source) (err error) {
	if err = s.Set("degraded", "sync_in_progress"); err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = s.Set("degraded", "partial_sync")
			_ = s.index()
		}
	}()
	const size = 100
	for offset := 0; ; offset += size {
		tickets, e := src.Tickets(ctx, offset, size)
		if e != nil {
			return e
		}
		if e = s.Page("tickets", "", tickets, fmt.Sprint(offset+len(tickets))); e != nil {
			return e
		}
		for _, t := range tickets {
			for co := 0; ; co += size {
				if co > 1000000 {
					return errors.New("comment pagination did not terminate")
				}
				comments, e := src.Comments(ctx, t, co, size)
				if e != nil {
					return e
				}
				if e = s.Page("comments", portal.ID(t), comments, fmt.Sprint(co+len(comments))); e != nil {
					return e
				}
				if len(comments) < size {
					break
				}
			}
		}
		if len(tickets) < size {
			break
		}
		if offset > 1000000 {
			return errors.New("pagination did not terminate")
		}
	}
	if err = s.index(); err != nil {
		return err
	}
	tx, e := s.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	_, e = tx.Exec(`INSERT INTO state VALUES('last_successful_sync',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, time.Now().UTC().Format(time.RFC3339))
	if e != nil {
		return e
	}
	_, e = tx.Exec(`INSERT INTO state VALUES('degraded','false') ON CONFLICT(key) DO UPDATE SET value=excluded.value`)
	if e != nil {
		return e
	}
	return tx.Commit()
}
