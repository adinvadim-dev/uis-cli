package archive

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"uis-cli/internal/portal"
)

type source struct{ fail bool }

func (s source) Tickets(context.Context, int, int) ([]portal.Object, error) {
	return []portal.Object{{"id": json.Number("123"), "context": "network outage", "status": "created", "is_ticket": true, "access_token": "NEVER_PERSIST"}}, nil
}
func (s source) Comments(context.Context, portal.Object, int, int) ([]portal.Object, error) {
	if s.fail {
		return nil, errors.New("failed thread")
	}
	return []portal.Object{{"id": json.Number("456"), "message": "router broken", "date_time": "2026-10-29", "files": []interface{}{map[string]interface{}{"id": 7, "filename": "a.txt", "url": "https://secret?token=abc"}}}}, nil
}
func TestOfflineArchiveIdempotentAndPartialFreshness(t *testing.T) {
	dir := t.TempDir()
	s, e := Open(dir)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Sync(context.Background(), source{}); e != nil {
		t.Fatal(e)
	}
	if e = s.Sync(context.Background(), source{}); e != nil {
		t.Fatal(e)
	}
	before, _ := s.Status()
	if e = s.Sync(context.Background(), source{fail: true}); e == nil {
		t.Fatal("partial sync accepted")
	}
	after, _ := s.Status()
	if before["last_successful_sync"] != after["last_successful_sync"] || after["degraded"] != true {
		t.Fatal("partial failure advanced freshness or concealed degradation")
	}
	s.Close()
	s, e = Open(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	rows, e := s.Search("router", "", 10, 0)
	if e != nil || len(rows) != 1 {
		t.Fatalf("offline comment FTS: %v %v", rows, e)
	}
	show, e := s.Show("123")
	if e != nil {
		t.Fatal(e)
	}
	comments := show["comments"].([]portal.Object)
	if len(comments) != 1 {
		t.Fatal("duplicate comments")
	}
	ticket := show["ticket"].(portal.Object)
	if ticket["access_token"] != nil {
		t.Fatal("token archived")
	}
	files := comments[0]["files"].([]interface{})
	file := files[0].(map[string]interface{})
	if file["url"] != nil || file["preservation_status"] != "pending" {
		t.Fatal("bearer URL persisted or missing preservation status")
	}
}
func TestPageRollbackDoesNotAdvanceCursor(t *testing.T) {
	s, e := Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	e = s.Page("tickets", "", []portal.Object{{"id": json.Number("123")}, {"context": "no ID"}}, "200")
	if e == nil {
		t.Fatal("missing ID accepted")
	}
	rows, _ := s.List("", "", 10, 1)
	if len(rows) != 0 {
		t.Fatal("page not atomic")
	}
	st, _ := s.Status()
	if len(st["checkpoints"].(map[string]string)) != 0 {
		t.Fatal("cursor advanced after rollback")
	}
}

func TestRecordingLocatorsAreNeverRenderedOrArchived(t *testing.T) {
	v := map[string]interface{}{"id": 123, "full_record_file_link": "secret", "call_records": []interface{}{"locator"}, "wav_call_records": []interface{}{"secret"}, "voice_mail_records": []interface{}{"secret"}, "header": map[string]interface{}{"talk_file": "secret", "voice_file": "secret", "download_url": "secret"}, "context": "preserved"}
	b, e := json.Marshal(Scrub(v))
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(b), "secret") || strings.Contains(string(b), "locator") {
		t.Fatalf("recording locator retained: %s", b)
	}
	if !strings.Contains(string(b), "preserved") {
		t.Fatal("ordinary fields lost")
	}
}

func TestScrubTypedLiveCommentSlice(t *testing.T) {
	v := map[string]interface{}{"comments": []portal.Object{{"files": []interface{}{map[string]interface{}{"filename": "a.txt", "url": "SECRET"}}}}}
	b, e := json.Marshal(Scrub(v))
	if e != nil || strings.Contains(string(b), "SECRET") {
		t.Fatalf("live slice not scrubbed: %s %v", b, e)
	}
}

func TestSearchPagination(t *testing.T) {
	s, e := Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	rows := []portal.Object{{"id": json.Number("1"), "context": "router"}, {"id": json.Number("2"), "context": "router"}}
	if e = s.Page("tickets", "", rows, "2"); e != nil {
		t.Fatal(e)
	}
	if e = s.index(); e != nil {
		t.Fatal(e)
	}
	first, e := s.Search("router", "", 1, 0)
	if e != nil {
		t.Fatal(e)
	}
	second, e := s.Search("router", "", 1, 1)
	if e != nil || len(first) != 1 || len(second) != 1 || portal.ID(first[0]) != "1" || portal.ID(second[0]) != "2" {
		t.Fatalf("pagination: %v %v %v", first, second, e)
	}
}

func TestSearchStatusFilterBeforePagination(t *testing.T) {
	s, e := Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	rows := []portal.Object{{"id": json.Number("1"), "context": "router", "status": "closed"}, {"id": json.Number("2"), "context": "router", "status": "created"}, {"id": json.Number("3"), "context": "router", "status": "created"}}
	if e = s.Page("tickets", "", rows, "3"); e != nil {
		t.Fatal(e)
	}
	if e = s.index(); e != nil {
		t.Fatal(e)
	}
	got, e := s.Search("router", "created", 1, 1)
	if e != nil || len(got) != 1 || portal.ID(got[0]) != "3" {
		t.Fatalf("status/pagination: %v %v", got, e)
	}
}
