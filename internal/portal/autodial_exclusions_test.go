package portal

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAutodialExclusion204AndReadback(t *testing.T) {
	present := false
	posts, deletes, reads, probes := 0, 0, 0, 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/auto_call/excluded_numa" || r.Header.Get("Authorization") != "Bearer fixture-jwt" {
			t.Fatal("wrong exclusion contract")
		}
		switch r.Method {
		case "POST":
			posts++
			if r.URL.Query().Get("numa") != "70000000000" {
				t.Fatal("missing query phone")
			}
			present = true
			w.WriteHeader(204)
		case "DELETE":
			deletes++
			present = false
			w.WriteHeader(204)
		case "GET":
			reads++
			if present {
				io.WriteString(w, `[{"id":"fixture","numa":"70000000000"}]`)
			} else {
				io.WriteString(w, `[]`)
			}
		default:
			t.Fatal("unexpected method")
		}
	}))
	defer s.Close()
	c := New(Session{CabinetToken: "fixture-jwt"}, time.Second)
	c.SupervisorURL = s.URL
	c.Verify = func(context.Context, Session) error { probes++; return nil }
	for _, a := range []string{"exclude", "include"} {
		v, e := c.Autodial(context.Background(), a, "", Object{"numa": "70000000000"})
		if e != nil || v.(map[string]interface{})["verified"] != true {
			t.Fatalf("write not reconciled: %v", e)
		}
	}
	if present || posts != 1 || deletes != 1 || reads != 2 || probes != 2 {
		t.Fatal("write repeated, missing preflight or state not restored")
	}
}
func TestAutodialExclusionAmbiguityNeverReplays(t *testing.T) {
	for _, tc := range []struct {
		writeStatus, readStatus, code int
		readBody                      string
	}{
		{503, 200, 9, `[]`}, {200, 200, 9, `[]`}, {204, 500, 9, `[]`}, {204, 200, 9, `[]`}, {204, 200, 9, `[{}]`}, {403, 200, 4, `[]`},
	} {
		posts := 0
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "POST" {
				posts++
				w.WriteHeader(tc.writeStatus)
				return
			}
			w.WriteHeader(tc.readStatus)
			io.WriteString(w, tc.readBody)
		}))
		c := New(Session{CabinetToken: "fixture-jwt"}, time.Second)
		c.SupervisorURL = s.URL
		c.Verify = func(context.Context, Session) error { return nil }
		_, e := c.Autodial(context.Background(), "exclude", "", Object{"numa": "70000000000"})
		var pe *Error
		if !errors.As(e, &pe) || pe.Code != tc.code || posts != 1 {
			t.Fatalf("unsafe write ambiguity: %v", e)
		}
		s.Close()
	}
}
