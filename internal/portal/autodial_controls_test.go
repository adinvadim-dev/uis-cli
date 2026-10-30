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

// Provider transition metadata is independent of the requested target. A
// planned campaign cannot be paused and a terminal campaign cannot be resumed.
func TestAutodialControlsRefuseUnsupportedTransitions(t *testing.T) {
	for _, tc := range []struct{ status, target string }{{"planned", "paused"}, {"cancelled", "in_process"}} {
		writes := 0
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "GET" {
				writes++
				w.WriteHeader(500)
				return
			}
			switch r.URL.Path {
			case "/auto_call/campaign":
				io.WriteString(w, `{"id":123,"status":"`+tc.status+`","phone_list":[]}`)
			case "/auto_call/campaign_status":
				io.WriteString(w, `[{"id":"planned","next_statuses":["in_process","cancelled"]},{"id":"cancelled","next_statuses":[]}]`)
			}
		}))
		c := New(Session{CabinetToken: "fixture-jwt"}, time.Second)
		c.SupervisorURL = s.URL
		_, e := c.Autodial(context.Background(), "status", "123", Object{"status": tc.target})
		var pe *Error
		if !errors.As(e, &pe) || pe.Stage != "campaign_transition_unavailable" || writes != 0 {
			t.Fatalf("unsafe status: %v", e)
		}
		s.Close()
	}
}
func TestAutodialClearProtectsFinalizedContacts(t *testing.T) {
	writes := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			writes++
		}
		io.WriteString(w, `{"id":123,"status":"paused","phone_list":[{"numa":"70000000000","status_mnemonic":"successfully_processed"},{"numa":"70000000001","status_mnemonic":"not_processed"}]}`)
	}))
	defer s.Close()
	c := New(Session{CabinetToken: "fixture-jwt"}, time.Second)
	c.SupervisorURL = s.URL
	_, e := c.Autodial(context.Background(), "clear", "123", nil)
	var pe *Error
	if !errors.As(e, &pe) || pe.Stage != "campaign_final_contact_cannot_be_removed" || writes != 0 {
		t.Fatalf("partial clear dispatched: %v", e)
	}
}
func TestAutodialTerminalNoOps(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Error("no-op wrote")
		}
		io.WriteString(w, `{"id":123,"status":"cancelled","phone_list":[{"numa":"70000000000","status_mnemonic":"deleted"}]}`)
	}))
	defer s.Close()
	c := New(Session{CabinetToken: "fixture-jwt"}, time.Second)
	c.SupervisorURL = s.URL
	for _, a := range []string{"status", "clear"} {
		p := Object{}
		if a == "status" {
			p["status"] = "cancelled"
		}
		v, e := c.Autodial(context.Background(), a, "123", p)
		if e != nil || v.(map[string]interface{})["no_op"] != true {
			t.Fatalf("failed no-op: %v", e)
		}
	}
}
